package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkMetrics(ctx context.Context, baseURL, runMode string) []*report.CheckResult {
	var results []*report.CheckResult

	// System Metrics (public)
	start := time.Now()
	resp, body, err := r.doRequest(ctx, baseURL, "GET", "/v1/metrics/system", nil, "")
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("System Metrics", "metrics", "pass", "Métricas do sistema", "/v1/metrics/system", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("System Metrics", "metrics", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/metrics/system", "GET", runMode, duration, nil, err, body))
	}

	// Queue Metrics (public)
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/metrics/queue", nil, "")
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Queue Metrics", "metrics", "pass", "Métricas da fila", "/v1/metrics/queue", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Queue Metrics", "metrics", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/metrics/queue", "GET", runMode, duration, nil, err, body))
	}

	return results
}