package service

import (
	"context"
	"fmt"
	"time"

	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/queue"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/pkg/cronparser"
)

// Erros de negócio exportados — o Handler decide o status HTTP de cada um.
var (
	ErrJobNotFound     = fmt.Errorf("job não encontrado")
	ErrJobLimitReached = fmt.Errorf("limite de jobs do plano atingido")
	ErrUnauthorized    = fmt.Errorf("job não pertence a este projeto")
	ErrInvalidSchedule = fmt.Errorf("schedule inválido")
)

type JobService struct {
	jobRepo  *postgres.JobRepository
	userRepo *postgres.UserRepository
	enqueuer *queue.Enqueuer
	cfg      *config.Config
}

func NewJobService(
	jobRepo *postgres.JobRepository,
	userRepo *postgres.UserRepository,
	enqueuer *queue.Enqueuer,
	cfg *config.Config,
) *JobService {
	return &JobService{
		jobRepo:  jobRepo,
		userRepo: userRepo,
		enqueuer: enqueuer,
		cfg:      cfg,
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

	// 3. Verifica limite de jobs do plano
	u, err := s.userRepo.FindUserByProjectID(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao buscar usuario: %w", err)
	}

	count, err := s.jobRepo.CountByProject(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao contar jobs: %w", err)
	}

	limit := u.MaxJobs(s.cfg.MaxJobsFreePlan, s.cfg.MaxJobsPaidPlan)
	if count >= limit {
		return nil, ErrJobLimitReached
	}

	// 4. Calcula o primeiro next_run_at
	nextRun, err := cronparser.NextRun(input.Schedule, input.Timezone, time.Now())
	if err != nil {
		return nil, fmt.Errorf("JobService.Create: erro ao calcular next_run: %w", err)
	}

	// 5. Monta a entidade e persiste
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

	created, err := s.jobRepo.Create(ctx, j)
	if err != nil {
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
	if j == nil {
		return nil, ErrJobNotFound
	}
	if j.ProjectID != projectID {
		return nil, ErrUnauthorized
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
