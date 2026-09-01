package checks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/config"
	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

type Runner struct {
	cfg        *config.Config
	client     *http.Client
	localClient *http.Client
	prodOK     bool
	localOK    bool
	mu         sync.Mutex
}

func NewRunner(cfg *config.Config) *Runner {
	return &Runner{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		localClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (r *Runner) Run(ctx context.Context) *report.Report {
	r.prodOK = r.checkTarget(ctx, r.cfg.BaseURL, "production")
	r.localOK = r.checkTarget(ctx, r.cfg.LocalBaseURL, "local")

	baseURL := r.cfg.BaseURL
	runMode := "production"
	if !r.prodOK && r.localOK {
		baseURL = r.cfg.LocalBaseURL
		runMode = "fallback-local"
	} else if !r.prodOK && !r.localOK {
		baseURL = r.cfg.BaseURL
		runMode = "both-down"
	}

	rep := report.NewReport(baseURL)
	rep.Diagnostics.ProductionAvailable = r.prodOK
	rep.Diagnostics.LocalAvailable = r.localOK
	rep.Diagnostics.FallbackUsed = runMode == "fallback-local"

	if runMode == "both-down" {
		rep.AddResult(&report.CheckResult{
			Name:     "Connectivity",
			Category: "infra",
			Status:   "fail",
			Message:  "Produção e local indisponíveis",
			Endpoint: r.cfg.BaseURL + " / " + r.cfg.LocalBaseURL,
			RunMode:  runMode,
		})
		return rep
	}

	// Use pre-configured test credentials or generate new ones
	testEmail := r.cfg.TestEmail
	testPassword := r.cfg.TestPassword
	testCPF := r.cfg.TestCPF
	
	// If pre-auth token provided, use it
	var sharedToken string
	var sharedProjectID string

	// Setup shared auth once
	if r.cfg.PreAuthToken != "" {
		sharedToken = r.cfg.PreAuthToken
		rep.AddResult(&report.CheckResult{
			Name:     "Pre-Auth Token",
			Category: "auth",
			Status:   "pass",
			Message:  "Token pré-configurado usado",
			RunMode:  runMode,
		})
	} else if r.prodOK {
		sharedToken, sharedProjectID = r.setupSharedAuth(ctx, baseURL, runMode, testEmail, testPassword, testCPF, rep)
	}

	suites := []struct {
		name string
		fn   func(context.Context, string, string, string, string) []*report.CheckResult
	}{
		{"Health & Infra", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			return r.checkHealthInfra(ctx, baseURL, runMode)
		}},
		{"Auth Flow", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			return r.checkAuthFlow(ctx, baseURL, runMode)
		}},
		{"Jobs CRUD", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "Jobs CRUD", Category: "jobs", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkJobsCRUD(ctx, baseURL, runMode, token)
		}},
		{"Projects CRUD", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "Projects CRUD", Category: "projects", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkProjectsCRUD(ctx, baseURL, runMode, token)
		}},
		{"API Keys CRUD", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "API Keys CRUD", Category: "keys", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkAPIKeysCRUD(ctx, baseURL, runMode, token)
		}},
		{"Telemetry", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "Telemetry", Category: "telemetry", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkTelemetry(ctx, baseURL, runMode, token)
		}},
		{"Webhooks", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			return r.checkWebhooks(ctx, baseURL, runMode)
		}},
		{"AI Health", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			return r.checkAI(ctx, baseURL, runMode)
		}},
		{"Email/Notifications", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "Email/Notifications", Category: "email", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkEmail(ctx, baseURL, runMode, token)
		}},
		{"Billing", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			if token == "" {
				return []*report.CheckResult{{Name: "Billing", Category: "billing", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
			}
			return r.checkBilling(ctx, baseURL, runMode, token)
		}},
		{"System Metrics", func(ctx context.Context, baseURL, runMode, token, projectID string) []*report.CheckResult {
			return r.checkMetrics(ctx, baseURL, runMode)
		}},
	}

	for _, suite := range suites {
		select {
		case <-ctx.Done():
			return rep
		default:
		}
		results := suite.fn(ctx, baseURL, runMode, sharedToken, sharedProjectID)
		for _, res := range results {
			rep.AddResult(res)
			stats := rep.Summary.ByCategory[res.Category]
			stats.Total++
			switch res.Status {
			case "pass":
				stats.Passed++
			case "fail":
				stats.Failed++
				if res.Category == "auth" || res.Category == "jobs" || res.Category == "infra" {
					rep.Summary.CriticalFails++
				}
			case "skip":
				stats.Skipped++
			}
			rep.Summary.ByCategory[res.Category] = stats
		}
	}

	if runMode == "fallback-local" && r.prodOK != r.localOK {
		rep.Diagnostics.FallbackResults = r.runQuickProdChecks(ctx)
	}

	return rep
}

func (r *Runner) setupSharedAuth(ctx context.Context, baseURL, runMode, email, password, cpf string, rep *report.Report) (string, string) {
	// Try login first
	loginBody := map[string]string{"email": email, "password": password}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/auth/login", loginBody, "")
	if err == nil && body != nil && resp.StatusCode == http.StatusOK {
		token := r.extractToken(body)
		projectID := r.extractProjectID(body)
		if token != "" {
			return token, projectID
		}
	}

	// Create user with valid CPF from config
	signupBody := map[string]string{
		"email":        email,
		"password":     password,
		"project_name": "HealthCheck Suite",
		"full_name":    "Health Check",
		"cpf":          cpf,
	}
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/signup", signupBody, "")
	if err != nil || body == nil {
		rep.AddResult(&report.CheckResult{
			Name:     "Shared Auth Setup",
			Category: "auth",
			Status:   "fail",
			Message:  fmt.Sprintf("Erro de conexão no signup: %v", err),
			Endpoint: "/v1/auth/signup",
			Method:   "POST",
			RunMode:  runMode,
		})
		return "", ""
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// Try login again in case user was created but returned conflict
		resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/login", loginBody, "")
		if err == nil && body != nil && resp.StatusCode == http.StatusOK {
			token := r.extractToken(body)
			projectID := r.extractProjectID(body)
			return token, projectID
		}
		rep.AddResult(&report.CheckResult{
			Name:     "Shared Auth Setup",
			Category: "auth",
			Status:   "fail",
			Message:  fmt.Sprintf("Signup falhou com status %d: %s", resp.StatusCode, string(body)),
			Endpoint: "/v1/auth/signup",
			Method:   "POST",
			RunMode:  runMode,
		})
		return "", ""
	}

	token := r.extractToken(body)
	projectID := r.extractProjectID(body)
	return token, projectID
}

func (r *Runner) checkTarget(ctx context.Context, url, label string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/v1/health", nil)
	resp, err := r.client.Do(req)
	if err != nil {
		fmt.Printf("[Checks] %s health check falhou: %v\n", label, err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (r *Runner) doRequest(ctx context.Context, baseURL, method, path string, body interface{}, token string) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Add Origin header to bypass CORS/middleware checks in production
	req.Header.Set("Origin", baseURL)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody, nil
}

func (r *Runner) makeResult(name, category, status, message, endpoint, method, runMode string, duration time.Duration, details map[string]interface{}, err error, respBody []byte) *report.CheckResult {
	res := &report.CheckResult{
		Name:     name,
		Category: category,
		Status:   status,
		Message:  message,
		Duration: duration,
		Endpoint: endpoint,
		Method:   method,
		RunMode:  runMode,
		Details:  details,
	}
	if err != nil {
		res.Error = err.Error()
	}
	if len(respBody) > 0 && len(respBody) < 2000 {
		res.Response = string(respBody)
	}
	return res
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:n]
}

func (r *Runner) extractToken(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	// Handle both signup response (token at root) and login response (token inside token object)
	if tokMap, ok := data["token"].(map[string]interface{}); ok {
		if tok, ok := tokMap["accessToken"].(string); ok {
			return tok
		}
	}
	if tok, ok := data["accessToken"].(string); ok {
		return tok
	}
	return ""
}

func (r *Runner) extractUserID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	if user, ok := data["user"].(map[string]interface{}); ok {
		if uid, ok := user["id"].(string); ok {
			return uid
		}
	}
	if uid, ok := data["id"].(string); ok {
		return uid
	}
	return ""
}

func (r *Runner) extractProjectID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	if projects, ok := data["projects"].([]interface{}); ok && len(projects) > 0 {
		if proj, ok := projects[0].(map[string]interface{}); ok {
			if pid, ok := proj["id"].(string); ok {
				return pid
			}
		}
	}
	return ""
}

func (r *Runner) extractID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	if id, ok := data["id"].(string); ok && id != "" {
		return id
	}
	if id, ok := data["ID"].(string); ok && id != "" {
		return id
	}
	return ""
}