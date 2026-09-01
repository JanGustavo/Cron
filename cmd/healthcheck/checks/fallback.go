package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) runQuickProdChecks(ctx context.Context) []*report.CheckResult {
	var results []*report.CheckResult
	baseURL := r.cfg.BaseURL
	runMode := "production-quick"

	// Quick health check
	start := time.Now()
	resp, _, err := r.doRequest(ctx, baseURL, "GET", "/v1/health", nil, "")
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Quick Prod Health", "infra", "pass", "Produção respondeu", "/v1/health", "GET", runMode, duration, nil, nil, nil))
	} else {
		results = append(results, r.makeResult("Quick Prod Health", "infra", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/health", "GET", runMode, duration, nil, err, nil))
	}

	// Quick AI check
	start = time.Now()
	resp, _, err = r.doRequest(ctx, baseURL, "GET", "/v1/health/ai", nil, "")
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Quick Prod AI", "ai", "pass", "IA OK", "/v1/health/ai", "GET", runMode, duration, nil, nil, nil))
	} else {
		results = append(results, r.makeResult("Quick Prod AI", "ai", "skip", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/health/ai", "GET", runMode, duration, nil, err, nil))
	}

	// Quick telemetry (no auth - should fail with 401)
	start = time.Now()
	resp, _, err = r.doRequest(ctx, baseURL, "GET", "/v1/executions/telemetry?time_range=1h", nil, "")
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		results = append(results, r.makeResult("Quick Prod Telemetry Auth", "telemetry", "pass", "Requer auth (401)", "/v1/executions/telemetry", "GET", runMode, duration, nil, nil, nil))
	} else {
		results = append(results, r.makeResult("Quick Prod Telemetry Auth", "telemetry", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/executions/telemetry", "GET", runMode, duration, nil, err, nil))
	}

	return results
}