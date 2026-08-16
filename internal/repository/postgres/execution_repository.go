package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

func (r *ExecutionRepository) ListByProject(
	ctx context.Context,
	projectID string,
	limit, offset int,
	search string,
	status string,
	startDate, endDate string,
) ([]*execution.ProjectExecution, int, error) {
	var args []interface{}
	args = append(args, projectID)
	argCount := 1

	whereClause := "WHERE j.project_id = $1"

	if status != "" {
		argCount++
		args = append(args, status)
		whereClause += fmt.Sprintf(" AND e.status = $%d", argCount)
	}

	if search != "" {
		argCount++
		args = append(args, "%"+search+"%")
		whereClause += fmt.Sprintf(" AND (j.name ILIKE $%d OR j.url ILIKE $%d OR e.id::text ILIKE $%d OR e.job_id::text ILIKE $%d)", argCount, argCount, argCount, argCount)
	}

	if startDate != "" {
		argCount++
		args = append(args, startDate)
		whereClause += fmt.Sprintf(" AND e.triggered_at >= $%d::timestamptz", argCount)
	}

	if endDate != "" {
		argCount++
		args = append(args, endDate+" 23:59:59.999")
		whereClause += fmt.Sprintf(" AND e.triggered_at <= $%d::timestamptz", argCount)
	}

	// 1. Query de contagem com mesmos filtros
	countQuery := fmt.Sprintf(`
		SELECT COUNT(e.id)
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		%s`, whereClause)

	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("ExecutionRepository.ListByProject count: %w", err)
	}

	// 2. Query de busca paginada e filtrada
	argCount++
	args = append(args, limit)
	limitArg := argCount

	argCount++
	args = append(args, offset)
	offsetArg := argCount

	query := fmt.Sprintf(`
		SELECT 
			e.id, e.job_id, e.status, e.http_status, e.duration_ms, 
			e.response_body, e.attempt_number, e.triggered_at,
			j.name as job_name, j.url as job_url
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		%s
		ORDER BY e.triggered_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, limitArg, offsetArg)

	rows, err := r.db.QueryContext(ctx, query, args...)
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

// DeleteExpiredExecutions deleta execuções antigas conforme a cota de retenção do plano de cada usuário.
func (r *ExecutionRepository) DeleteExpiredExecutions(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM executions e
		WHERE e.triggered_at < NOW() - (
			COALESCE(
				(
					SELECT p.logs_retention_days
					FROM plans p
					JOIN subscriptions s ON s.plan_code = p.code
					JOIN projects proj ON proj.user_id = s.user_id
					JOIN jobs j ON j.project_id = proj.id
					WHERE j.id = e.job_id
					  AND s.status IN ('active', 'trialing')
				),
				(
					SELECT p.logs_retention_days
					FROM plans p
					WHERE p.code = 'free'
				)
			) || ' days'
		)::INTERVAL
	`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("ExecutionRepository.DeleteExpiredExecutions: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

type FailedJobDigest struct {
	JobID               string
	JobName             string
	Schedule            string
	URL                 string
	HTTPMethod          string
	ConsecutiveFailures int
	FailureCount        int
	LastHTTPStatus      int
	LastResponseBody    string
	LastTriggeredAt     string
}

func (r *ExecutionRepository) GetFailedExecutionsForUserLast24Hours(ctx context.Context, userID string) ([]*FailedJobDigest, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT 
			j.id as job_id, 
			j.name as job_name, 
			j.schedule, 
			j.url, 
			j.http_method, 
			j.consecutive_failures,
			COUNT(e.id) as failure_count,
			COALESCE((SELECT http_status FROM executions WHERE job_id = j.id AND status = 'failed' ORDER BY triggered_at DESC LIMIT 1), 0) as last_http_status,
			COALESCE((SELECT response_body FROM executions WHERE job_id = j.id AND status = 'failed' ORDER BY triggered_at DESC LIMIT 1), '') as last_response_body,
			(SELECT triggered_at FROM executions WHERE job_id = j.id AND status = 'failed' ORDER BY triggered_at DESC LIMIT 1) as last_triggered_at
		FROM jobs j
		INNER JOIN projects p ON p.id = j.project_id
		INNER JOIN executions e ON e.job_id = j.id
		WHERE p.user_id = $1
		  AND e.status = 'failed'
		  AND e.triggered_at >= NOW() - INTERVAL '24 hours'
		GROUP BY j.id, j.name, j.schedule, j.url, j.http_method, j.consecutive_failures
		ORDER BY last_triggered_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ExecutionRepository.GetFailedExecutionsForUserLast24Hours: %w", err)
	}
	defer rows.Close()

	var digests []*FailedJobDigest
	for rows.Next() {
		d := &FailedJobDigest{}
		var lastTriggeredAt time.Time
		if err := rows.Scan(
			&d.JobID, &d.JobName, &d.Schedule, &d.URL, &d.HTTPMethod,
			&d.ConsecutiveFailures, &d.FailureCount, &d.LastHTTPStatus, &d.LastResponseBody, &lastTriggeredAt,
		); err != nil {
			return nil, fmt.Errorf("ExecutionRepository.GetFailedExecutionsForUserLast24Hours scan: %w", err)
		}
		d.LastTriggeredAt = lastTriggeredAt.Format(time.RFC3339)
		digests = append(digests, d)
	}
	return digests, nil
}
