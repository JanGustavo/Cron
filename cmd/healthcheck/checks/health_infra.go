package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkHealthInfra(ctx context.Context, baseURL, runMode string) []*report.CheckResult {
	var results []*report.CheckResult

	checks := []struct {
		name     string
		path     string
		category string
		expected int
	}{
		{"Health Root", "/health", "infra", http.StatusOK},
		{"Health V1", "/v1/health", "infra", http.StatusOK},
		{"AI Health", "/v1/health/ai", "infra", http.StatusOK},
		{"System Metrics", "/v1/metrics/system", "infra", http.StatusOK},
		{"Queue Metrics", "/v1/metrics/queue", "infra", http.StatusOK},
	}

	for _, c := range checks {
		start := time.Now()
		resp, body, err := r.doRequest(ctx, baseURL, "GET", c.path, nil, "")
		duration := time.Since(start)

		if err != nil {
			results = append(results, r.makeResult(c.name, c.category, "fail", err.Error(), c.path, "GET", runMode, duration, nil, err, nil))
			continue
		}

		if resp.StatusCode == c.expected {
			results = append(results, r.makeResult(c.name, c.category, "pass", fmt.Sprintf("Status %d", resp.StatusCode), c.path, "GET", runMode, duration, map[string]interface{}{"status": resp.StatusCode}, nil, body))
		} else {
			results = append(results, r.makeResult(c.name, c.category, "fail", fmt.Sprintf("Status %d (esperado %d)", resp.StatusCode, c.expected), c.path, "GET", runMode, duration, map[string]interface{}{"status": resp.StatusCode, "expected": c.expected}, nil, body))
		}
	}

	return results
}