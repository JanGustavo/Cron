package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/pkg/cronparser"
)

// Erros de negócio exportados — o Handler decide o status HTTP de cada um.
var (
	ErrJobNotFound            = fmt.Errorf("job não encontrado")
	ErrJobLimitReached        = fmt.Errorf("limite de jobs do plano atingido")
	ErrUnauthorized           = fmt.Errorf("job não pertence a este projeto")
	ErrInvalidSchedule        = fmt.Errorf("schedule inválido")
	ErrWorkflowsDisabled      = fmt.Errorf("workflows (encadeamento de tarefas) são exclusivos do Plano PRO. Faça o upgrade para utilizar")
	ErrWebhookAlertsDisabled  = fmt.Errorf("alertas via webhook, Slack ou Discord são exclusivos do Plano PRO. Faça o upgrade para utilizar")
)

type JobService struct {
	jobRepo           *postgres.JobRepository
	userRepo          *postgres.UserRepository
	EntitlementEngine *EntitlementEngine
	enqueuer          *queue.Enqueuer
	cfg               *config.Config
}

func NewJobService(
	jobRepo *postgres.JobRepository,
	userRepo *postgres.UserRepository,
	entitlementEngine *EntitlementEngine,
	enqueuer *queue.Enqueuer,
	cfg *config.Config,
) *JobService {
	return &JobService{
		jobRepo:           jobRepo,
		userRepo:          userRepo,
		EntitlementEngine: entitlementEngine,
		enqueuer:          enqueuer,
		cfg:               cfg,
	}
}

// CreateJobInput sao os dados que chegam do Handler.
// Separado da entidade Job para nao expor campos internos (ID, timestamps, etc).
type CreateJobInput struct {
	ProjectID       string
	Name            string
	Schedule        string
	Timezone        string
	URL             string
	HTTPMethod      job.HTTPMethod
	Headers         map[string]string
	Payload         map[string]any
	WebhookAlertURL *string
	NextJobID       *string
	Tags            []string
}

// Create valida, aplica regras de negocio e persiste um novo job.
func (s *JobService) Create(ctx context.Context, input CreateJobInput) (*job.Job, error) {
	// 1. Valida o schedule antes de qualquer coisa
	if err := cronparser.Validate(input.Schedule); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSchedule, err.Error())
	}

	// 2. Valida timezone
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}

	// 3. Busca usuário e limites dinâmicos do plano
	u, err := s.userRepo.FindUserByProjectID(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao buscar usuario: %w", err)
	}

	limits, err := s.EntitlementEngine.GetUserLimits(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao obter limites do plano: %w", err)
	}

	// 3.5. Valida restrições de features do plano (Workflows e Alertas)
	if input.NextJobID != nil && *input.NextJobID != "" {
		if !limits.WorkflowsEnabled {
			return nil, ErrWorkflowsDisabled
		}
		if s.detectCycle(ctx, "", *input.NextJobID) {
			return nil, fmt.Errorf("ciclo de workflow detectado: o destino %s geraria um loop de execução infinito", *input.NextJobID)
		}
	}
	if input.WebhookAlertURL != nil && *input.WebhookAlertURL != "" && !limits.AlertsWebhooksEnabled {
		return nil, ErrWebhookAlertsDisabled
	}

	// 4. Calcula o primeiro next_run_at
	nextRun, err := cronparser.NextRun(input.Schedule, input.Timezone, time.Now())
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao calcular next_run: %w", err)
	}

	// 5. Monta a entidade
	j := &job.Job{
		ProjectID:       input.ProjectID,
		Name:            input.Name,
		Schedule:        input.Schedule,
		Timezone:        input.Timezone,
		URL:             input.URL,
		HTTPMethod:      input.HTTPMethod,
		Headers:         input.Headers,
		Payload:         input.Payload,
		Status:          job.StatusActive,
		NextRunAt:       nextRun,
		WebhookAlertURL: input.WebhookAlertURL,
		NextJobID:       input.NextJobID,
		Tags:            input.Tags,
	}

	// 6. Persiste sob advisory lock transacional garantindo exclusão mútua e cotas precisas
	created, err := s.jobRepo.CreateWithLock(ctx, j, u.ID, limits.MaxJobs)
	if err != nil {
		if strings.Contains(err.Error(), "limit_reached") {
			return nil, ErrJobLimitReached
		}
		return nil, fmt.Errorf("JobService.Create: %w", err)
	}

	return created, nil
}

// List retorna todos os jobs de um projeto.
func (s *JobService) List(ctx context.Context, projectID string) ([]*job.Job, error) {
	jobs, err := s.jobRepo.ListByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("JobService.List: %w", err)
	}
	return jobs, nil
}

// GetByID busca um job garantindo que pertence ao projeto autenticado.
func (s *JobService) GetByID(ctx context.Context, id, projectID string) (*job.Job, error) {
	j, err := s.jobRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("JobService.GetByID: %w", err)
	}
	if j == nil || j.ProjectID != projectID {
		// Retorna uniformemente "não encontrado" para evitar enumeração de IDs existentes de outros tenants
		return nil, ErrJobNotFound
	}
	return j, nil
}

// UpdateStatus pausa ou reativa um job.
func (s *JobService) UpdateStatus(ctx context.Context, id, projectID string, status job.Status) error {
	j, err := s.GetByID(ctx, id, projectID)
	if err != nil {
		return err
	}

	// Ao reativar / agendar: recalcula next_run_at a partir de agora, reseta falhas e limpa last_run_at
	if status == job.StatusActive {
		nextRun, err := cronparser.NextRun(j.Schedule, j.Timezone, time.Now())
		if err != nil {
			return fmt.Errorf("JobService.UpdateStatus: %w", err)
		}
		if err := s.jobRepo.ReactivateJob(ctx, id, nextRun); err != nil {
			return fmt.Errorf("JobService.UpdateStatus: %w", err)
		}
		return nil
	}

	return s.jobRepo.UpdateStatus(ctx, id, status)
}

// Delete remove um job verificando ownership.
func (s *JobService) Delete(ctx context.Context, id, projectID string) error {
	_, err := s.GetByID(ctx, id, projectID)
	if err != nil {
		return err
	}
	return s.jobRepo.Delete(ctx, id, projectID)
}

// TriggerNow dispara o enfileiramento imediato no Redis para execução do job.
func (s *JobService) TriggerNow(ctx context.Context, jobID, projectID string) error {
	// 1. Busca o job e garante isolamento multi-tenant (segurança!) reutilizando o GetByID
	j, err := s.GetByID(ctx, jobID, projectID)
	if err != nil {
		return err
	}

	// 2. Dispara o enfileiramento imediato no Redis para o Worker executar
	return s.enqueuer.Enqueue(ctx, j.ID)
}

// UpdateJobInput são os dados para atualizar um job.
type UpdateJobInput struct {
	ID              string
	ProjectID       string
	Name            string
	Schedule        string
	Timezone        string
	URL             string
	HTTPMethod      string
	Headers         map[string]string
	Payload         map[string]any
	WebhookAlertURL *string
	NextJobID       *string
	Tags            []string
}

// Update atualiza as configurações de um job no banco, validando permissões e recalculando o next_run_at.
func (s *JobService) Update(ctx context.Context, input UpdateJobInput) (*job.Job, error) {
	j, err := s.GetByID(ctx, input.ID, input.ProjectID)
	if err != nil {
		return nil, err
	}

	// Busca usuário e limites dinâmicos do plano
	u, err := s.userRepo.FindUserByProjectID(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Update: erro ao buscar usuario: %w", err)
	}

	limits, err := s.EntitlementEngine.GetUserLimits(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Update: erro ao obter limites do plano: %w", err)
	}

	// Valida restrições de features do plano (Workflows e Alertas)
	if input.NextJobID != nil && *input.NextJobID != "" {
		if !limits.WorkflowsEnabled {
			return nil, ErrWorkflowsDisabled
		}
		if s.detectCycle(ctx, input.ID, *input.NextJobID) {
			return nil, fmt.Errorf("ciclo de workflow detectado: encadear o job %s no destino %s geraria um loop infinito de execução", input.ID, *input.NextJobID)
		}
	}
	if input.WebhookAlertURL != nil && *input.WebhookAlertURL != "" && !limits.AlertsWebhooksEnabled {
		return nil, ErrWebhookAlertsDisabled
	}

	// Se o schedule mudou, valida e recalcula a próxima execução
	if j.Schedule != input.Schedule || j.Timezone != input.Timezone {
		nextRun, err := cronparser.NextRun(input.Schedule, input.Timezone, time.Now())
		if err != nil {
			return nil, ErrInvalidSchedule
		}
		j.NextRunAt = nextRun
	}

	j.Name = input.Name
	j.Schedule = input.Schedule
	j.Timezone = input.Timezone
	j.URL = input.URL
	j.HTTPMethod = job.HTTPMethod(input.HTTPMethod)
	j.Headers = input.Headers
	j.Payload = input.Payload
	j.WebhookAlertURL = input.WebhookAlertURL
	j.NextJobID = input.NextJobID
	j.Tags = input.Tags

	if err := s.jobRepo.Update(ctx, j); err != nil {
		return nil, fmt.Errorf("JobService.Update: %w", err)
	}

	return j, nil
}

// detectCycle verifica se atribuir targetNextJobID ao job startJobID criaria um ciclo/loop infinito no workflow.
func (s *JobService) detectCycle(ctx context.Context, startJobID, targetNextJobID string) bool {
	if startJobID != "" && startJobID == targetNextJobID {
		return true // Auto-referência direta (A ➔ A)
	}

	visited := make(map[string]bool)
	current := targetNextJobID

	for current != "" {
		if startJobID != "" && current == startJobID {
			return true // Ciclo detectado (ex: A ➔ B ➔ A)
		}
		if visited[current] {
			return true // Loop pré-existente no caminho
		}
		visited[current] = true

		nextJob, err := s.jobRepo.FindByID(ctx, current)
		if err != nil || nextJob == nil || nextJob.NextJobID == nil || *nextJob.NextJobID == "" {
			break
		}
		current = *nextJob.NextJobID
	}

	return false
}
