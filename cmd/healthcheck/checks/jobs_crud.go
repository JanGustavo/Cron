package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/report"
)

func (r *Runner) checkJobsCRUD(ctx context.Context, baseURL, runMode, token string) []*report.CheckResult {
	var results []*report.CheckResult

	if token == "" {
		return []*report.CheckResult{{Name: "Jobs CRUD", Category: "jobs", Status: "skip", Message: "Sem token de auth", RunMode: runMode}}
	}

	var jobID string

	// 1. Create Job
	start := time.Now()
	jobBody := map[string]interface{}{
		"name":        "Health Check Job",
		"schedule":    "*/5 * * * *",
		"timezone":    "America/Sao_Paulo",
		"url":         "https://httpbin.org/post",
		"http_method": "POST",
		"headers":     map[string]string{"X-Healthcheck": "true"},
		"payload":     map[string]string{"check": "health"},
	}
	resp, body, err := r.doRequest(ctx, baseURL, "POST", "/v1/jobs", jobBody, token)
	duration := time.Since(start)
	if err == nil && resp.StatusCode == http.StatusCreated {
		jobID = r.extractID(body)
		results = append(results, r.makeResult("Jobs Create", "jobs", "pass", fmt.Sprintf("Job criado: %s", jobID), "/v1/jobs", "POST", runMode, duration, map[string]interface{}{"job_id": jobID}, nil, body))
	} else if err == nil && (resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden) {
		// Plan limit reached - expected behavior for free plan
		results = append(results, r.makeResult("Jobs Create", "jobs", "skip", fmt.Sprintf("Limite do plano atingido (status %d)", resp.StatusCode), "/v1/jobs", "POST", runMode, duration, nil, nil, body))
		return results
	} else {
		results = append(results, r.makeResult("Jobs Create", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs", "POST", runMode, duration, nil, err, body))
		return results
	}

	// 2. List Jobs
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/jobs", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Jobs List", "jobs", "pass", "Lista carregada", "/v1/jobs", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs List", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs", "GET", runMode, duration, nil, err, body))
	}

	// 3. Get Job by ID
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/jobs/"+jobID, nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Jobs Get", "jobs", "pass", "Job obtido", "/v1/jobs/{id}", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs Get", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}", "GET", runMode, duration, nil, err, body))
	}

	// 4. Trigger Job
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "POST", "/v1/jobs/"+jobID+"/trigger", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Jobs Trigger", "jobs", "pass", "Job disparado", "/v1/jobs/{id}/trigger", "POST", runMode, duration, nil, nil, body))
		time.Sleep(3 * time.Second)
	} else {
		results = append(results, r.makeResult("Jobs Trigger", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}/trigger", "POST", runMode, duration, nil, err, body))
	}

	// 5. List Executions
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "GET", "/v1/jobs/"+jobID+"/executions?limit=10", nil, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Jobs Executions List", "jobs", "pass", "Execuções listadas", "/v1/jobs/{id}/executions", "GET", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs Executions List", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}/executions", "GET", runMode, duration, nil, err, body))
	}

	// 6. Update Job Status (pause)
	start = time.Now()
	pauseBody := map[string]string{"status": "paused"}
	resp, body, err = r.doRequest(ctx, baseURL, "PATCH", "/v1/jobs/"+jobID, pauseBody, token)
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent) {
		results = append(results, r.makeResult("Jobs Pause", "jobs", "pass", "Job pausado", "/v1/jobs/{id}", "PATCH", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs Pause", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}", "PATCH", runMode, duration, nil, err, body))
	}

	// 7. Update Job (PUT)
	start = time.Now()
	updateBody := map[string]interface{}{
		"name":        "Health Check Job Updated",
		"schedule":    "*/10 * * * *",
		"timezone":    "America/Sao_Paulo",
		"url":         "https://httpbin.org/post",
		"http_method": "POST",
	}
	resp, body, err = r.doRequest(ctx, baseURL, "PUT", "/v1/jobs/"+jobID, updateBody, token)
	duration = time.Since(start)
	if err == nil && resp.StatusCode == http.StatusOK {
		results = append(results, r.makeResult("Jobs Update", "jobs", "pass", "Job atualizado", "/v1/jobs/{id}", "PUT", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs Update", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}", "PUT", runMode, duration, nil, err, body))
	}

	// 8. Delete Job
	start = time.Now()
	resp, body, err = r.doRequest(ctx, baseURL, "DELETE", "/v1/jobs/"+jobID, nil, token)
	duration = time.Since(start)
	if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent) {
		results = append(results, r.makeResult("Jobs Delete", "jobs", "pass", "Job removido", "/v1/jobs/{id}", "DELETE", runMode, duration, nil, nil, body))
	} else {
		results = append(results, r.makeResult("Jobs Delete", "jobs", "fail", fmt.Sprintf("Status %d", resp.StatusCode), "/v1/jobs/{id}", "DELETE", runMode, duration, nil, err, body))
	}

	return results
}

