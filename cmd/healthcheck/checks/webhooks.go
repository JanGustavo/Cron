package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkWebhooks(ctx context.Context, baseURL, runMode string) []*report.CheckResult {
	var results []*report.CheckResult

	// Test Stripe webhook endpoint (public, no auth)
	start := time.Now()
	stripePayload := map[string]interface{}{
		"id":   "evt_test_healthcheck",
		"type": "checkout.session.completed",
	}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/billing/webhook", stripePayload, "")
	duration := time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest) {
		// BadRequest is OK - signature verification fails for test payload
		results = append(results, r.makeResult("Stripe Webhook Endpoint", "webhooks", "pass", fmt.Sprintf("Endpoint acessível (status %d)", resp.StatusCode), "/v1/billing/webhook", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Stripe Webhook Endpoint", "webhooks", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/billing/webhook", "POST", runMode, duration, nil, err, body))
	}

	return results
}