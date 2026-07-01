package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/job"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// Create insere um novo job e retorna o registro completo com ID e timestamps.
func (r *JobRepository) Create(ctx context.Context, j *job.Job) (*job.Job, error) {
	headers, _ := json.Marshal(j.Headers) // _ para ignorar erro, 
	payload, _ := json.Marshal(j.Payload)

	query := `
		INSERT INTO jobs 
			(project_id, name, schedule, timezone, url, http_method, headers, payload, next_run_at, webhook_alert_url)
		VALUES 
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		j.ProjectID,
		j.Name,
		j.Schedule,
		j.Timezone,
		j.URL,
		j.HTTPMethod,
		headers,
		payload,
		j.NextRunAt,
		j.WebhookAlertURL,
	).Scan(&j.ID, &j.CreatedAt, &j.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("JobRepository.Create: %w", err)
	}

	return j, nil
}

// FindByID busca um job pelo ID.
// Retorna nil, nil se não encontrado — o Service decide o que fazer com isso.
func (r *JobRepository) FindByID(ctx context.Context, id string) (*job.Job, error) {
	query := `
		SELECT id, project_id, name, schedule, timezone, url, http_method,
		       headers, payload, status, next_run_at, last_run_at, consecutive_failures,
		       webhook_alert_url, created_at, updated_at
		FROM jobs WHERE id = $1`

	j := &job.Job{}
	var headers, payload []byte
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&j.ID, &j.ProjectID, &j.Name, &j.Schedule, &j.Timezone,
		&j.URL, &j.HTTPMethod, &headers, &payload, &j.Status, &j.NextRunAt, &j.LastRunAt,
		&j.ConsecutiveFailures, &j.WebhookAlertURL, &j.CreatedAt, &j.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("JobRepository.FindByID: %w", err)
	}

	json.Unmarshal(headers, &j.Headers)
	json.Unmarshal(payload, &j.Payload)

	return j, nil
}

// ListByProject retorna todos os jobs de um projeto, mais recentes primeiro.
func (r *JobRepository) ListByProject(ctx context.Context, projectID string) ([]*job.Job, error) {
	query := `
		SELECT id, project_id, name, schedule, timezone, url, http_method,
		       headers, payload, status, next_run_at, last_run_at, consecutive_failures,
		       webhook_alert_url, created_at, updated_at
		FROM jobs WHERE project_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("JobRepository.ListByProject: %w", err)
	}
	defer rows.Close() // SEMPRE fechar rows — vaza conexão se não fechar

	var jobs []*job.Job
	for rows.Next() {
		j := &job.Job{}
		var headers, payload []byte
		err := rows.Scan(
			&j.ID, &j.ProjectID, &j.Name, &j.Schedule, &j.Timezone,
			&j.URL, &j.HTTPMethod, &headers, &payload, &j.Status, &j.NextRunAt, &j.LastRunAt,
			&j.ConsecutiveFailures, &j.WebhookAlertURL, &j.CreatedAt, &j.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("JobRepository.ListByProject scan: %w", err)
		}
		json.Unmarshal(headers, &j.Headers)
		json.Unmarshal(payload, &j.Payload)
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// FindEligibleToRun é a query mais crítica do sistema — chamada pelo Scheduler a cada 30s.
// Usa o índice parcial idx_jobs_scheduler para performance.
func (r *JobRepository) FindEligibleToRun(ctx context.Context, now time.Time) ([]*job.Job, error) {
	query := `
		SELECT id, project_id, url, http_method, headers, payload, schedule, timezone, next_run_at
		FROM jobs
		WHERE status = 'active' AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT 500`

	rows, err := r.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("JobRepository.FindEligibleToRun: %w", err)
	}
	defer rows.Close()

	var jobs []*job.Job
	for rows.Next() {
		j := &job.Job{}
		var headers, payload []byte
		err := rows.Scan(
			&j.ID, &j.ProjectID, &j.URL, &j.HTTPMethod,
			&headers, &payload, &j.Schedule, &j.Timezone, &j.NextRunAt,
		)
		if err != nil {
			return nil, fmt.Errorf("JobRepository.FindEligibleToRun scan: %w", err)
		}
		json.Unmarshal(headers, &j.Headers)
		json.Unmarshal(payload, &j.Payload)
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// UpdateNextRun atualiza o próximo horário de execução após enfileirar.
func (r *JobRepository) UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET next_run_at = $1, last_run_at = NOW(), updated_at = NOW() WHERE id = $2`,
		nextRun, id,
	)
	return err
}

// UpdateStatus altera o status (active, paused, failing).
func (r *JobRepository) UpdateStatus(ctx context.Context, id string, status job.Status) error {
	var err error
	if status == job.StatusActive {
		_, err = r.db.ExecContext(ctx,
			`UPDATE jobs SET status = $1, consecutive_failures = 0, updated_at = NOW() WHERE id = $2`,
			status, id,
		)
	} else {
		_, err = r.db.ExecContext(ctx,
			`UPDATE jobs SET status = $1, updated_at = NOW() WHERE id = $2`,
			status, id,
		)
	}
	return err
}

// IncrementFailures incrementa o contador e marca como 'failing' se >= 3.
func (r *JobRepository) IncrementFailures(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET
			consecutive_failures = consecutive_failures + 1,
			status = CASE WHEN consecutive_failures + 1 >= 3 THEN 'failing' ELSE status END,
			last_run_at = NOW(),
			updated_at = NOW()
		WHERE id = $1`, id,
	)
	return err
}

// ResetFailures limpa o contador após sucesso.
func (r *JobRepository) ResetFailures(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET consecutive_failures = 0, status = 'active', last_run_at = NOW(), updated_at = NOW() WHERE id = $1`,
		id,
	)
	return err
}

// Delete remove o job — só funciona se pertencer ao projeto (segurança multi-tenant).
func (r *JobRepository) Delete(ctx context.Context, id, projectID string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM jobs WHERE id = $1 AND project_id = $2`,
		id, projectID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("job não encontrado ou não pertence ao projeto")
	}
	return nil
}

// CountByProject retorna quantos jobs ativos um projeto tem.
// Usado pelo Service para verificar limite de plano.
func (r *JobRepository) CountByProject(ctx context.Context, projectID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE project_id = $1 AND status != 'paused'`,
		projectID,
	).Scan(&count)
	return count, err
}

// Update atualiza as configurações de um job no banco.
func (r *JobRepository) Update(ctx context.Context, j *job.Job) error {
	headers, _ := json.Marshal(j.Headers)
	payload, _ := json.Marshal(j.Payload)

	query := `
		UPDATE jobs SET
			name = $1,
			schedule = $2,
			timezone = $3,
			url = $4,
			http_method = $5,
			headers = $6,
			payload = $7,
			webhook_alert_url = $8,
			next_run_at = $9,
			updated_at = NOW()
		WHERE id = $10`

	_, err := r.db.ExecContext(ctx, query,
		j.Name,
		j.Schedule,
		j.Timezone,
		j.URL,
		j.HTTPMethod,
		headers,
		payload,
		j.WebhookAlertURL,
		j.NextRunAt,
		j.ID,
	)
	if err != nil {
		return fmt.Errorf("JobRepository.Update: %w", err)
	}
	return nil
}
