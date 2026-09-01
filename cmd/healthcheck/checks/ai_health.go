package checks

import (
	"encoding/json"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkAI(ctx context.Context, baseURL, runMode string) []*report.CheckResult {
	var results []*report.CheckResult

	start := time.Now()
	resp, body, err := r.doRequest(ctx, baseURL, "GET", "/v1/health/ai", nil, "")
	duration := time.Since(start)

	if err == nil && resp.StatusCode == http.StatusOK {
		var data map[string]interface{}
		json.Unmarshal(body, &data)
		status := "unknown"
		if s, ok := data["status"].(string); ok {
			status = s
		}
		results = append(results, r.makeResult("AI Health Check", "ai", "pass", fmt.Sprintf("Status: %s", status), "/v1/health/ai", "GET", runMode, duration, data, nil, body))
	} else if err == nil && resp.StatusCode == http.StatusServiceUnavailable {
		results = append(results, r.makeResult("AI Health Check", "ai", "skip", "IA desabilitada ou indisponível", "/v1/health/ai", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("AI Health Check", "ai", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/health/ai", "GET", runMode, duration, nil, err, body))
	}

	return results
}