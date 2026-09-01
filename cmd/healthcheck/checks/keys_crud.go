package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkAPIKeysCRUD(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "API Keys CRUD", Category: "keys", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	var keyID string

	// 1. Create API Key
	start := time.Now()
	keyBody := map[string]string{"name": "Health Check Key"}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/keys", keyBody, token)
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusCreated {
		keyID = r.extractID(body)
		results = append(results, r.makeResult("Keys Create", "keys", "pass", fmt.Sprintf("Key criada: %s", keyID), "/v1/keys", "POST", runMode, duration, map[string]interface{}{"key_id": keyID}, nil, body))
	} else {
		results = append(results, r.makeResult("Keys Create", "keys", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/keys", "POST", runMode, duration, nil, err, body))
		return results
	}

	// 2. List API Keys
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/keys", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Keys List", "keys", "pass", "Keys listadas", "/v1/keys", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Keys List", "keys", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/keys", "GET", runMode, duration, nil, err, body))
	}

	// 3. Delete API Key
	if keyID != "" {
		start = time.Now()
		resp, body, err = r.doRequest(ctx, baseURL, "DELETE", "/v1/keys/"+keyID, nil, token)
		duration = time.Since(start)
		if err == nil && resp.StatusCode == http.StatusOK {
			results = append(results, r.makeResult("Keys Delete", "keys", "pass", "Key removida", "/v1/keys/{id}", "DELETE", runMode, duration, nil, nil, body))
		} else {
			results = append(results, r.makeResult("Keys Delete", "keys", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/keys/{id}", "DELETE", runMode, duration, nil, err, body))
		}
	}

	return results
}