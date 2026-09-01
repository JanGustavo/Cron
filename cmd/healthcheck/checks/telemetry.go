package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkTelemetry(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "Telemetry", Category: "telemetry", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	ranges := []string{"1h", "6h", "24h", "7d", "30d"}
	for _, tr := range ranges {
		start := time.Now()
		path := fmt.Sprintf("/v1/executions/telemetry?time_range=%s&granularity=auto", tr)
		resp, body, err := r.doRequest(ctx, baseURL, "GET", path, nil, token)
		duration := time.Since(start)

		if err == nil && resp.StatusCode == http.StatusOK {
			results = append(results, r.makeResult(fmt.Sprintf("Telemetry %s", tr), "telemetry", "pass", "Dados retornados", path, "GET", runMode, duration, nil, nil, body))
		} else {
			results = append(results, r.makeResult(fmt.Sprintf("Telemetry %s", tr), "telemetry", "fail", fmt.Sprintf("Status %d", resp.StatusCode), path, "GET", runMode, duration, nil, err, body))
		}
	}

	// Global executions list
	start := time.Now()
	resp, body, err := r.doRequest(ctx, baseURL, "GET", "/v1/executions?limit=10", nil, token)
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Executions Global List", "telemetry", "pass", "Lista global", "/v1/executions", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Executions Global List", "telemetry", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/executions", "GET", runMode, duration, nil, err, body))
	}

	return results
}