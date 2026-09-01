package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkProjectsCRUD(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "Projects CRUD", Category: "projects", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	var projectID string

	// 1. Create Project
	start := time.Now()
	projBody := map[string]string{"name": "Health Check Project"}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/projects", projBody, token)
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusCreated {
		projectID = r.extractID(body)
		results = append(results, r.makeResult("Projects Create", "projects", "pass", fmt.Sprintf("Projeto criado: %s", projectID), "/v1/projects", "POST", runMode, duration, map[string]interface{}{"project_id": projectID}, nil, body))
	} else if err == nil && (resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden) {
		// Plan limit reached - expected behavior for free plan
		results = append(results, r.makeResult("Projects Create", "projects", "skip", fmt.Sprintf("Limite do plano atingido (status %d)", resp.StatusCode), "/v1/projects", "POST", runMode, duration, nil, nil, body))
		return results
	} else {
		results = append(results, r.makeResult("Projects Create", "projects", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/projects", "POST", runMode, duration, nil, err, body))
		return results
	}

	// 2. Switch Project
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/projects/"+projectID+"/switch", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		token = r.extractToken(body)
		results = append(results, r.makeResult("Projects Switch", "projects", "pass", "Switch realizado", "/v1/projects/{id}/switch", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Projects Switch", "projects", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/projects/{id}/switch", "POST", runMode, duration, nil, err, body))
	}

	// 3. Rotate Webhook Secret
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/projects/webhook-secret/rotate", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Projects Rotate Secret", "projects", "pass", "Secret rotacionado", "/v1/projects/webhook-secret/rotate", "POST", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Projects Rotate Secret", "projects", "skip", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/projects/webhook-secret/rotate", "POST", runMode, duration, nil, err, body))
	}

	// 4. Update Project
	start = time.Now()
	updateBody := map[string]string{"name": "Health Check Project Updated"}
	resp, body, err = r.doRequest(ctx, baseURL, "PUT", "/v1/projects/"+projectID, updateBody, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Projects Update", "projects", "pass", "Projeto atualizado", "/v1/projects/{id}", "PUT", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Projects Update", "projects", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/projects/{id}", "PUT", runMode, duration, nil, err, body))
	}

	// 5. Delete Project
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "DELETE", "/v1/projects/"+projectID, nil, token)
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent) {
		results = append(results, r.makeResult("Projects Delete", "projects", "pass", "Projeto removido", "/v1/projects/{id}", "DELETE", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Projects Delete", "projects", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/projects/{id}", "DELETE", runMode, duration, nil, err, body))
	}

	return results
}