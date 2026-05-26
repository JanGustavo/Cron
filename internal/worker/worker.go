package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"

	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
	"github.com/JanGustavo/Cron/pkg/httputil"
)

type Worker struct {
	jobRepo       *postgres.JobRepository
	executionRepo *postgres.ExecutionRepository
	alertService  *service.AlertService
}

func New(
	jobRepo *postgres.JobRepository,
	executionRepo *postgres.ExecutionRepository,
	alertService *service.AlertService,
) *Worker {
	return &Worker{
		jobRepo:       jobRepo,
		executionRepo: executionRepo,
		alertService:  alertService,
	}
}

// ProcessTask é o handler registrado no Asynq para tasks do tipo "http:job".
// O Asynq chama esse método para cada task consumida da fila.
// Retornar error = Asynq agenda retry automático com backoff exponencial.
// Retornar nil = sucesso, task removida da fila.
func (w *Worker) ProcessTask(ctx context.Context, t *asynq.Task) error {
	// Desserializa o payload da task
	var p queue.HTTPJobPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		// Payload corrompido — não tem retry, descarta direto
		return fmt.Errorf("worker: payload inválido: %w", asynq.SkipRetry)
	}

	// Busca os detalhes completos do job no banco
	j, err := w.jobRepo.FindByID(ctx, p.JobID)
	if err != nil {
		return fmt.Errorf("worker: erro ao buscar job %s: %w", p.JobID, err)
	}
	if j == nil {
		// Job foi deletado enquanto estava na fila — descarta sem retry
		log.Printf("worker: job %s não encontrado — descartando task", p.JobID)
		return nil
	}

	if j.Status == job.StatusFailing || j.Status == job.StatusPaused {
		log.Printf("worker: job %s está inativo/suspenso (status: %s) — descartando execução", p.JobID, j.Status)
		return nil
	}

	// Timeout: respeita o padrão de 30s do MVP
	timeout := 30 * time.Second

	// Número da tentativa atual (Asynq disponibiliza via GetRetryCount)
	retryInfo, _ := asynq.GetRetryCount(ctx)
	attemptNumber := retryInfo + 1

	log.Printf("worker: executando job %s — tentativa %d — %s %s",
		j.ID, attemptNumber, j.HTTPMethod, j.URL)

	// Executa o HTTP request
	result, err := httputil.Execute(ctx, string(j.HTTPMethod), j.URL, j.Headers, j.Payload, timeout)

	execStatus := execution.StatusSuccess
	var httpStatus *int
	responseBody := ""

	if err != nil {
		// Falha de rede/DNS/timeout — registra e retorna erro para retry
		execStatus = execution.StatusFailed
		responseBody = err.Error()
		log.Printf("worker: job %s falhou (rede/timeout): %v", j.ID, err)
	} else {
		httpStatus = &result.StatusCode
		responseBody = result.Body

		if result.StatusCode >= 400 {
			// HTTP de erro — registra e retorna erro para retry
			execStatus = execution.StatusFailed
			log.Printf("worker: job %s retornou HTTP %d", j.ID, result.StatusCode)
		} else {
			log.Printf("worker: job %s executado com sucesso — HTTP %d em %dms",
				j.ID, result.StatusCode, result.DurationMs)
		}
	}

	// Persiste o resultado no banco
	exec := &execution.Execution{
		JobID:         j.ID,
		Status:        execStatus,
		HTTPStatus:    httpStatus,
		DurationMs:    func() int {
			if result != nil { return result.DurationMs }
			return 0
		}(),
		ResponseBody:  responseBody,
		AttemptNumber: attemptNumber,
	}
	if err := w.executionRepo.Create(ctx, exec); err != nil {
		log.Printf("worker: erro ao salvar execution do job %s: %v", j.ID, err)
		// Não retorna erro aqui — salvar o log não pode derrubar a execução
	}

	// Se falhou: atualiza contador de falhas e verifica alerta
	if execStatus == execution.StatusFailed {
		if err := w.jobRepo.IncrementFailures(ctx, j.ID); err != nil {
			log.Printf("worker: erro ao incrementar falhas do job %s: %v", j.ID, err)
		}

		// Dispara webhook de alerta se atingiu 3 falhas e tem URL configurada
		updatedJob, _ := w.jobRepo.FindByID(ctx, j.ID)
		if updatedJob != nil && updatedJob.ShouldAlert() {
			statusCode := 0
			if httpStatus != nil {
				statusCode = *httpStatus
			}
			w.alertService.Notify(*updatedJob.WebhookAlertURL, j.ID,
				updatedJob.ConsecutiveFailures, statusCode, responseBody)
		}

		// Se o job entrou em estado de falha (status = failing ou consecutiveFailures >= 3),
		// devemos instruir o Asynq a NÃO fazer mais retries dessa execução!
		if updatedJob != nil && (updatedJob.Status == job.StatusFailing || updatedJob.ConsecutiveFailures >= 3) {
			log.Printf("worker: job %s atingiu o limite de falhas consecutivas (%d) — suspendendo e cancelando retries da fila", j.ID, updatedJob.ConsecutiveFailures)
			return fmt.Errorf("job %s suspenso após %d falhas: %w", j.ID, updatedJob.ConsecutiveFailures, asynq.SkipRetry)
		}

		return fmt.Errorf("job %s falhou — HTTP status: %v", j.ID, httpStatus)
	}

	// Sucesso: limpa o contador de falhas
	if err := w.jobRepo.ResetFailures(ctx, j.ID); err != nil {
		log.Printf("worker: erro ao resetar falhas do job %s: %v", j.ID, err)
	}

	return nil
}
