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
