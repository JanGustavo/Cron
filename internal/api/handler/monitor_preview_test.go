package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

type previewReadOnlyRepository struct {
	*postgres.MonitorRepository
	belongs bool
	project string
}

func (*previewReadOnlyRepository) ListRulesByKey(ctx context.Context, projectID, key string) ([]*monitor.MonitorRule, error) {
	return []*monitor.MonitorRule{{ID: "condition", Key: key, Operator: monitor.OpEq, ThresholdValue: "9007199254740993", AlertEmail: true}}, nil
}
func (r *previewReadOnlyRepository) JobBelongsToProject(_ context.Context, project, _ string) (bool, error) {
	r.project = project
	return r.belongs, nil
}

func TestPreviewHTTPContract(t *testing.T) {
	const secret = "test-preview-secret"
	token, err := auth.GenerateToken("user", "test@example.com", "project", "free", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewMonitorHandler(service.NewMonitorService(&previewReadOnlyRepository{}, nil, secret))
	protected := middleware.Auth(nil, secret)(http.HandlerFunc(handler.PreviewKey))
	tests := []struct {
		name, body    string
		authenticated bool
		status        int
	}{
		{"precise typed value", `{"key":"count","value":9007199254740993}`, true, 200},
		{"null sample allowed", `{"key":"count","value":null}`, true, 200},
		{"missing sample rejected", `{"key":"count"}`, true, 400},
		{"empty key rejected", `{"key":" ","value":0}`, true, 400},
		{"trailing JSON rejected", `{"key":"count","value":0} {}`, true, 400},
		{"malformed job", `{"job_id":"bad","key":"count","value":0}`, true, 400},
		{"foreign job", `{"job_id":"f034c234-57d5-49f9-b466-e0e072037e93","key":"count","value":0}`, true, 400},
		{"oversize sample", `{"key":"count","value":"` + strings.Repeat("x", 256*1024) + `"}`, true, 400},
		{"authentication required", `{"key":"count","value":0}`, false, 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/monitor/preview", strings.NewReader(tc.body))
			if tc.authenticated {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			recorder := httptest.NewRecorder()
			protected.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, recorder.Code, recorder.Body.String())
			}
			if tc.name == "precise typed value" {
				var result monitor.CheckResult
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if !result.Evaluated || result.Violated || string(result.Rules[0].ObservedValue) != "9007199254740993" {
					t.Fatalf("precision lost: %+v", result)
				}
			}
		})
	}
}

func TestCandidatePreviewHTTPContract(t *testing.T) {
	const secret = "candidate-secret"
	token, err := auth.GenerateToken("user", "test@example.com", "project", "free", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repo := &previewReadOnlyRepository{belongs: true}
	handler := NewMonitorHandler(service.NewMonitorService(repo, nil, secret))
	protected := middleware.Auth(nil, secret)(http.HandlerFunc(handler.PreviewRule))
	// Match the frontend request mapper's casing while preserving tagged snake names.
	body := `{"Rule":{"Job_id":"f034c234-57d5-49f9-b466-e0e072037e93","Project_id":"foreign","Name":"Count","Key":"/count","Mode":"require_success","Value_type":"number","Operator":"eq","Threshold_value":"9007199254740993"},"Response_body":"{\"count\":9007199254740993}"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/monitor/preview-rule", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("candidate rejected: %s", recorder.Body.String())
	}
	var result struct {
		Evaluations []monitor.RuleStatus `json:"evaluations"`
		Passed      bool                 `json:"passed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Passed || len(result.Evaluations) != 1 || string(result.Evaluations[0].ObservedValue) != "9007199254740993" || repo.project != "project" {
		t.Fatalf("candidate lost scope/precision: %+v project=%s", result, repo.project)
	}
}

type optInRepository struct{ *previewReadOnlyRepository }

func (*optInRepository) CreateRule(_ context.Context, rule *monitor.MonitorRule) (*monitor.MonitorRule, error) {
	return rule, nil
}

func TestJobValidationRequiresExplicitActivation(t *testing.T) {
	const secret = "test-monitor-default-secret"
	token, err := auth.GenerateToken("user", "test@example.com", "project", "free", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := NewMonitorHandler(service.NewMonitorService(&optInRepository{&previewReadOnlyRepository{belongs: true}}, nil, secret))
	protected := middleware.Auth(nil, secret)(http.HandlerFunc(h.CreateRule))
	for _, tc := range []struct {
		name, extra string
		enabled     bool
	}{
		{"job omitted activation", `,"job_id":"f034c234-57d5-49f9-b466-e0e072037e93"`, false},
		{"job explicit activation", `,"job_id":"f034c234-57d5-49f9-b466-e0e072037e93","is_enabled":true`, true},
		{"job explicit deactivation", `,"job_id":"f034c234-57d5-49f9-b466-e0e072037e93","is_enabled":false`, false},
		{"API metrics unchanged", ``, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/monitor/rules", strings.NewReader(`{"name":"Status","key":"/status","operator":"eq","threshold_value":"ok","value_type":"string"`+tc.extra+`}`))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var rule monitor.MonitorRule
			if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
				t.Fatal(err)
			}
			if rule.IsEnabled != tc.enabled {
				t.Fatalf("enabled=%v want=%v", rule.IsEnabled, tc.enabled)
			}
		})
	}
}
