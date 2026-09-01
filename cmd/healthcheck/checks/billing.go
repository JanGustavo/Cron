package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkBilling(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "Billing", Category: "billing", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	// Test Checkout Session
	start := time.Now()
	checkoutBody := map[string]string{
		"price_id":     "price_test",
		"success_url":  "https://cronflow.jangustavo.me/success",
		"cancel_url":   "https://cronflow.jangustavo.me/cancel",
	}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/billing/checkout", checkoutBody, token)
	duration := time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest) {
		results = append(results, r.makeResult("Billing Checkout Session", "billing", "pass", fmt.Sprintf("Endpoint respondeu (status %d)", resp.StatusCode), "/v1/billing/checkout", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Billing Checkout Session", "billing", "skip", fmt.Sprintf("Status %d (Stripe pode não estar configurado)", resp.StatusCode), "/v1/billing/checkout", "POST", runMode, duration, nil, err, body))
	}

	// Test Portal Session
	start = time.Now()
	portalBody := map[string]string{"return_url": "https://cronflow.jangustavo.me/profile"}
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/billing/portal", portalBody, token)
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest) {
		results = append(results, r.makeResult("Billing Portal Session", "billing", "pass", fmt.Sprintf("Endpoint respondeu (status %d)", resp.StatusCode), "/v1/billing/portal", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Billing Portal Session", "billing", "skip", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/billing/portal", "POST", runMode, duration, nil, err, body))
	}

	return results
}