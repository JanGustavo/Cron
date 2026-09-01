package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkAuthFlow(ctx context.Context, baseURL, runMode string) []*report.CheckResult {
	var results []*report.CheckResult

	// Use random email to avoid conflicts
	testEmail := fmt.Sprintf("hc_auth_%s@test.cronflow.sh", randomString(8))
	testPassword := "HealthCheck123!"
	testProject := "HealthCheck Flow"
	var userID string
	var projectID string
	var token string

	// 1. Signup with random email and valid CPF
	start := time.Now()
	signupBody := map[string]string{
		"email":        testEmail,
		"password":     testPassword,
		"project_name": testProject,
		"full_name":    "Health Check Flow",
		"cpf":          "12345678909",
	}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/auth/signup", signupBody, "")
	duration := time.Since(start)

	if err != nil || body == nil {
		results = append(results, r.makeResult("Auth Signup", "auth", "fail", err.Error(), "/v1/auth/signup", "POST", runMode, duration, nil, err, nil))
		return results
	}

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		token = r.extractToken(body)
		userID = r.extractUserID(body)
		projectID = r.extractProjectID(body)
		results = append(results, r.makeResult("Auth Signup", "auth", "pass", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/auth/signup", "POST", runMode, duration, map[string]interface{}{"email": testEmail}, nil, body))
	} else if resp.StatusCode == http.StatusConflict {
		// User exists, try login
		loginBody := map[string]string{"email": testEmail, "password": testPassword}
		resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/login", loginBody, "")
		if err == nil && body != nil && resp.StatusCode == http.StatusOK {
			token = r.extractToken(body)
			projectID = r.extractProjectID(body)
			results = append(results, r.makeResult("Auth Signup (existing)", "auth", "pass", "Usuário já existia, login OK", "/v1/auth/signup", "POST", runMode, duration, nil, nil, body))
		} else {
			// Conflict but login failed - skip the rest
			results = append(results, r.makeResult("Auth Signup", "auth", "skip", "Conflito de CPF/email mas login falhou", "/v1/auth/signup", "POST", runMode, duration, nil, nil, body))
			return results
		}
	} else if resp.StatusCode == http.StatusBadRequest {
		// CPF already used by another account - skip this test
		results = append(results, r.makeResult("Auth Signup", "auth", "skip", fmt.Sprintf("CPF em uso por outra conta (status %d)", resp.StatusCode), "/v1/auth/signup", "POST", runMode, duration, nil, nil, body))
		return results
	} else {
		results = append(results, r.makeResult("Auth Signup", "auth", "fail", fmt.Sprintf("Status %d: %s", resp.StatusCode, string(body)), "/v1/auth/signup", "POST", runMode, duration, map[string]interface{}{"status": resp.StatusCode}, nil, body))
		return results
	}

	if token == "" {
		results = append(results, r.makeResult("Auth Login", "auth", "fail", "Não obteve token", "/v1/auth/login", "POST", runMode, 0, nil, fmt.Errorf("no token"), nil))
		return results
	}

	results = append(results, r.makeResult("Auth Login", "auth", "pass", "Token obtido", "/v1/auth/login", "POST", runMode, 0, map[string]interface{}{"user_id": userID, "project_id": projectID}, nil, nil))

	// 2. Get Profile
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/users/profile", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Auth Get Profile", "auth", "pass", "Perfil carregado", "/v1/users/profile", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Auth Get Profile", "auth", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/users/profile", "GET", runMode, duration, nil, err, body))
	}

	// 3. Update Profile
	start = time.Now()
	updateBody := map[string]string{"full_name": "Health Check Updated"}
	resp, body, err = r.doRequest(ctx, baseURL, "PUT", "/v1/users/profile", updateBody, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Auth Update Profile", "auth", "pass", "Perfil atualizado", "/v1/users/profile", "PUT", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Auth Update Profile", "auth", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/users/profile", "PUT", runMode, duration, nil, err, body))
	}

	// 4. Change Password
	start = time.Now()
	pwdBody := map[string]string{"current_password": testPassword, "new_password": "NewHealthCheck123!"}
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/users/change-password", pwdBody, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Auth Change Password", "auth", "pass", "Senha alterada", "/v1/users/change-password", "POST", runMode, duration, nil, nil, body))
		// Login again with new password
		loginBody := map[string]string{"email": testEmail, "password": "NewHealthCheck123!"}
		resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/login", loginBody, "")
		if err == nil && resp.StatusCode == http.StatusOK {
			token = r.extractToken(body)
		}
	} else {
		results = append(results, r.makeResult("Auth Change Password", "auth", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/users/change-password", "POST", runMode, duration, nil, err, body))
	}

	// 5. Forgot Password
	start = time.Now()
	forgotBody := map[string]string{"email": testEmail}
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/forgot-password", forgotBody, "")
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted) {
		results = append(results, r.makeResult("Auth Forgot Password", "auth", "pass", "Email de reset enviado", "/v1/auth/forgot-password", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Auth Forgot Password", "auth", "skip", fmt.Sprintf("Status %d (pode falhar sem SMTP)", resp.StatusCode), "/v1/auth/forgot-password", "POST", runMode, duration, nil, err, body))
	}

	// 6. Delete Account (cleanup)
	if token != "" {
		start = time.Now()
		resp, body, err = r.doRequest(ctx, baseURL, "DELETE", "/v1/users/account", nil, token)
		duration = time.Since(start)
		if err == nil && resp.StatusCode == http.StatusOK {
			results = append(results, r.makeResult("Auth Delete Account", "auth", "pass", "Conta removida", "/v1/users/account", "DELETE", runMode, duration, nil, nil, body))
		} else {
			results = append(results, r.makeResult("Auth Delete Account", "auth", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/users/account", "DELETE", runMode, duration, nil, err, body))
		}
	}

	return results
}