package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkEmail(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "Email/Notifications", Category: "email", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	// Test forgot password (triggers email)
	start := time.Now()
	testEmail := "healthcheck@test.cronflow.sh"
	forgotBody := map[string]string{"email": testEmail}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/auth/forgot-password", forgotBody, "")
	duration := time.Since(start)

	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted) {
		results = append(results, r.makeResult("Email Forgot Password", "email", "pass", "Endpoint de reset respondeu", "/v1/auth/forgot-password", "POST", runMode, duration, nil, nil, body))
	} else if err == nil && resp.StatusCode == http.StatusInternalServerError {
		results = append(results, r.makeResult("Email Forgot Password", "email", "skip", "SMTP não configurado", "/v1/auth/forgot-password", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Email Forgot Password", "email", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/auth/forgot-password", "POST", runMode, duration, nil, err, body))
	}

	// Test resend verification
	start = time.Now()
	verifyBody := map[string]string{"email": testEmail}
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/auth/resend-verification", verifyBody, "")
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted) {
		results = append(results, r.makeResult("Email Resend Verification", "email", "pass", "Reenvio verificação OK", "/v1/auth/resend-verification", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Email Resend Verification", "email", "skip", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/auth/resend-verification", "POST", runMode, duration, nil, err, body))
	}

	return results
}