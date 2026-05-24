package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/JanGustavo/Cron/internal/domain/execution"
)

type ExecutionRepository struct {
	db *sql.DB
}

func NewExecutionRepository(db *sql.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

func (r *ExecutionRepository) Create(ctx context.Context, e *execution.Execution) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO executions
			(job_id, status, http_status, duration_ms, response_body, attempt_number)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.JobID, e.Status, e.HTTPStatus, e.DurationMs, e.ResponseBody, e.AttemptNumber,
	)
	if err != nil {
		return fmt.Errorf("ExecutionRepository.Create: %w", err)
	}
	return nil
}

func (r *ExecutionRepository) ListByJob(ctx context.Context, jobID string, limit int) ([]*execution.Execution, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, job_id, status, http_status, duration_ms, response_body, attempt_number, triggered_at
		FROM executions
		WHERE job_id = $1
		ORDER BY triggered_at DESC
		LIMIT $2`,
		jobID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ExecutionRepository.ListByJob: %w", err)
	}
	defer rows.Close()

	var executions []*execution.Execution
	for rows.Next() {
		e := &execution.Execution{}
		if err := rows.Scan(
			&e.ID, &e.JobID, &e.Status, &e.HTTPStatus,
			&e.DurationMs, &e.ResponseBody, &e.AttemptNumber, &e.TriggeredAt,
		); err != nil {
			return nil, fmt.Errorf("ExecutionRepository.ListByJob scan: %w", err)
		}
		executions = append(executions, e)
	}
	return executions, nil
}

func (r *ExecutionRepository) ListByProject(ctx context.Context, projectID string, limit, offset int) ([]*execution.ProjectExecution, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(e.id)
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		WHERE j.project_id = $1`,
		projectID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("ExecutionRepository.ListByProject count: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT 
			e.id, e.job_id, e.status, e.http_status, e.duration_ms, 
			e.response_body, e.attempt_number, e.triggered_at,
			j.name as job_name, j.url as job_url
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		WHERE j.project_id = $1
		ORDER BY e.triggered_at DESC
		LIMIT $2 OFFSET $3`,
		projectID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("ExecutionRepository.ListByProject query: %w", err)
	}
	defer rows.Close()

	var executions []*execution.ProjectExecution
	for rows.Next() {
		pe := &execution.ProjectExecution{}
		var httpStatus sql.NullInt64
		
		err := rows.Scan(
			&pe.ID, &pe.JobID, &pe.Status, &httpStatus, &pe.DurationMs,
			&pe.ResponseBody, &pe.AttemptNumber, &pe.TriggeredAt,
			&pe.JobName, &pe.JobURL,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("ExecutionRepository.ListByProject scan: %w", err)
		}
		
		if httpStatus.Valid {
			val := int(httpStatus.Int64)
			pe.HTTPStatus = &val
		}
		
		executions = append(executions, pe)
	}

	return executions, total, nil
}

// DeleteOlderThan deleta execuções mais antigas que N dias.
// Chamado pelo job de limpeza diário.
func (r *ExecutionRepository) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM executions WHERE triggered_at < NOW() - ($1 || ' days')::INTERVAL`,
		days,
	)
	if err != nil {
		return 0, fmt.Errorf("ExecutionRepository.DeleteOlderThan: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}
