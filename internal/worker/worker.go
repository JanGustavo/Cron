package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/pkg/httputil"
)

type workerJobs interface {
	FindByID(context.Context, string) (*job.Job, error)
	ExecutionLimits(context.Context, string) (*billing.PlanLimits, error)
	IncrementFailures(context.Context, string) error
	ResetFailures(context.Context, string) error
}
type workerExecutions interface {
	CreateWithRuleEvaluations(context.Context, *execution.Execution, []monitor.RuleStatus) error
}
type workerEnqueuer interface {
	Enqueue(context.Context, string) error
}
type workerMonitor interface {
	EvaluateJobResponse(context.Context, string, string, monitor.Response) (bool, []monitor.RuleStatus, error)
}

type httpExecutor func(context.Context, string, string, map[string]string, map[string]any, time.Duration) (*httputil.Result, error)

type Worker struct {
	executeHTTP    httpExecutor
	jobRepo        workerJobs
	executionRepo  workerExecutions
	alertService   *service.AlertService
	enqueuer       workerEnqueuer
	jwtSecret      string
	monitorService workerMonitor
}

func New(jobRepo *postgres.JobRepository, executionRepo *postgres.ExecutionRepository, alertService *service.AlertService, enqueuer *queue.Enqueuer, jwtSecret string, monitorService *service.MonitorService) *Worker {
	worker := &Worker{jobRepo: jobRepo, executionRepo: executionRepo, alertService: alertService, enqueuer: enqueuer, jwtSecret: jwtSecret, executeHTTP: httputil.Execute}
	if monitorService != nil {
		worker.monitorService = monitorService
	}
	return worker
}

func (w *Worker) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p queue.HTTPJobPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("worker: payload inválido: %w", asynq.SkipRetry)
	}

	j, err := w.jobRepo.FindByID(ctx, p.JobID)
	if err != nil {
		return fmt.Errorf("worker: erro ao buscar job %s: %w", p.JobID, err)
	}
	if j == nil {
		log.Printf("worker: job %s não encontrado — descartando task", p.JobID)
		return nil
	}
	limits, err := w.jobRepo.ExecutionLimits(ctx, j.ProjectID)
	if err != nil {
		return fmt.Errorf("worker: entitlement check: %w", err)
	}
	// Reconciliation may have paused this queued job after its grace deadline.
	j, err = w.jobRepo.FindByID(ctx, p.JobID)
	if err != nil {
		return err
	}
	if j == nil {
		return nil
	}
	if j.Status == job.StatusFailing || j.Status == job.StatusPaused {
		log.Printf("worker: job %s está inativo/suspenso (status: %s) — descartando execução", p.JobID, j.Status)
		return nil
	}

	timeout := 30 * time.Second
	retryInfo, _ := asynq.GetRetryCount(ctx)
	attemptNumber := retryInfo + 1

	j.URL = replacePlaceholders(j.URL, j, attemptNumber)
	if j.Headers != nil {
		updatedHeaders := make(map[string]string)
		for k, v := range j.Headers {
			updatedHeaders[k] = replacePlaceholders(v, j, attemptNumber)
		}
		j.Headers = updatedHeaders
	}
	if j.Payload != nil {
		j.Payload = replacePayloadPlaceholders(j.Payload, j, attemptNumber)
	}

	log.Printf("worker: executando job %s — tentativa %d — %s %s", j.ID, attemptNumber, j.HTTPMethod, j.URL)
	result, err := w.executeHTTP(ctx, string(j.HTTPMethod), j.URL, j.Headers, j.Payload, timeout)

	execStatus := execution.StatusSuccess
	var httpStatus *int
	responseBody := ""
	if err != nil {
		execStatus = execution.StatusFailed
		responseBody = err.Error()
		log.Printf("worker: job %s falhou (rede/timeout): %v", j.ID, err)
	} else {
		httpStatus = &result.StatusCode
		responseBody = httputil.ResponsePreview(result.Body)
		if result.StatusCode >= 400 || result.CaptureError == "body_read_error" {
			execStatus = execution.StatusFailed
			log.Printf("worker: job %s retornou HTTP %d", j.ID, result.StatusCode)
		} else {
			log.Printf("worker: job %s executado com sucesso — HTTP %d em %dms", j.ID, result.StatusCode, result.DurationMs)
		}
	}

	var ruleEvaluations []monitor.RuleStatus
	if w.monitorService != nil {
		observation := monitor.Response{Received: result != nil}
		if result != nil {
			observation.Body = []byte(result.Body)
			observation.CaptureError = result.CaptureError
		}
		_, ruleEvaluations, err = w.monitorService.EvaluateJobResponse(ctx, j.ProjectID, j.ID, observation)
		if err != nil {
			log.Printf("worker: erro ao avaliar regras de monitoramento do job %s: %v", j.ID, err)
			ruleEvaluations = []monitor.RuleStatus{{RuleName: "Monitoramento", Status: "error", Reason: "evaluation_unavailable", Message: "Não foi possível carregar as condições de monitoramento"}}
		}
	}

	transportStatus := execStatus
	if transportStatus == execution.StatusSuccess && !monitor.SuccessConditionsPassed(ruleEvaluations) {
		execStatus = execution.StatusValidationFailed
	}
	exec := &execution.Execution{
		TransportStatus: transportStatus,
		JobID:           j.ID,
		Status:          execStatus,
		HTTPStatus:      httpStatus,
		DurationMs: func() int {
			if result != nil {
				return result.DurationMs
			}
			return 0
		}(),
		ResponseBody:  responseBody,
		AttemptNumber: attemptNumber,
	}
	if err := w.executionRepo.CreateWithRuleEvaluations(ctx, exec, ruleEvaluations); err != nil {
		log.Printf("worker: erro ao salvar execution do job %s: %v", j.ID, err)
	}

	if transportStatus == execution.StatusFailed {
		if err := w.jobRepo.IncrementFailures(ctx, j.ID); err != nil {
			log.Printf("worker: erro ao incrementar falhas do job %s: %v", j.ID, err)
		}
		updatedJob, _ := w.jobRepo.FindByID(ctx, j.ID)
		if limits.AlertsWebhooksEnabled && updatedJob != nil && updatedJob.ShouldAlert() {
			statusCode := 0
			if httpStatus != nil {
				statusCode = *httpStatus
			}
			w.alertService.Notify(*updatedJob.WebhookAlertURL, j.ID, j.Name, updatedJob.ConsecutiveFailures, statusCode, responseBody, updatedJob.ProjectID, w.jwtSecret)
		}
		if updatedJob != nil && updatedJob.ConsecutiveFailures == 4 {
			statusCode := 0
			if httpStatus != nil {
				statusCode = *httpStatus
			}
			w.alertService.NotifyEmail(j.ID, j.Name, updatedJob.ConsecutiveFailures, statusCode, responseBody, updatedJob.ProjectID)
		}
		if updatedJob != nil && (updatedJob.Status == job.StatusFailing || updatedJob.ConsecutiveFailures >= 4) {
			log.Printf("worker: job %s atingiu o limite de falhas consecutivas (%d) — suspendendo e cancelando retries da fila", j.ID, updatedJob.ConsecutiveFailures)
			return fmt.Errorf("job %s suspenso após %d falhas: %w", j.ID, updatedJob.ConsecutiveFailures, asynq.SkipRetry)
		}
		return fmt.Errorf("job %s falhou — HTTP status: %v", j.ID, httpStatus)
	}

	if err := w.jobRepo.ResetFailures(ctx, j.ID); err != nil {
		log.Printf("worker: erro ao resetar falhas do job %s: %v", j.ID, err)
	}
	if limits.WorkflowsEnabled && execStatus == execution.StatusSuccess && j.NextJobID != nil && *j.NextJobID != "" {
		if *j.NextJobID == j.ID {
			log.Printf("worker: auto-referência detectada no job %s — interrompendo loop infinito de execução", j.ID)
		} else {
			log.Printf("worker: job %s concluído com sucesso — enfileirando próximo job %s", j.ID, *j.NextJobID)
			if err := w.enqueuer.Enqueue(ctx, *j.NextJobID); err != nil {
				log.Printf("worker: erro ao enfileirar próximo job %s: %v", *j.NextJobID, err)
			}
		}
	}
	return nil
}

func replacePlaceholders(val string, j *job.Job, attempt int) string {
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	val = strings.ReplaceAll(val, "{{cronflow.timestamp}}", nowStr)
	val = strings.ReplaceAll(val, "{{cronflow.job_id}}", j.ID)
	val = strings.ReplaceAll(val, "{{cronflow.run_id}}", fmt.Sprintf("%s-%d", j.ID, attempt))
	return val
}

func replacePayloadPlaceholders(payload map[string]any, j *job.Job, attempt int) map[string]any {
	if payload == nil {
		return nil
	}
	res := make(map[string]any)
	for k, v := range payload {
		switch val := v.(type) {
		case string:
			res[k] = replacePlaceholders(val, j, attempt)
		case map[string]any:
			res[k] = replacePayloadPlaceholders(val, j, attempt)
		default:
			res[k] = v
		}
	}
	return res
}
