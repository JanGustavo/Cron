package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/lib/pq"
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

// CleanupExpiredUnverifiedUsers calls the stored procedure to delete users who did not verify their email after 24 hours.
func (r *ExecutionRepository) CleanupExpiredUnverifiedUsers(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `CALL pr_cleanup_expired_unverified_users()`)
	if err != nil {
		return fmt.Errorf("ExecutionRepository.CleanupExpiredUnverifiedUsers: %w", err)
	}
	return nil
}

type TelemetryBucket struct {
	BucketTime   time.Time `json:"bucket_time"`
	Volume       int       `json:"volume"`
	SuccessCount int       `json:"success_count"`
	FailedCount  int       `json:"failed_count"`
	AvgLatency   int       `json:"avg_latency"`
	MaxLatency   int       `json:"max_latency"`
	FailedJobs   []string  `json:"failed_jobs"`
}

type TelemetrySummary struct {
	TotalVolume  int     `json:"total_volume"`
	SuccessCount int     `json:"success_count"`
	FailedCount  int     `json:"failed_count"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatency   int     `json:"avg_latency"`
	MaxLatency   int     `json:"max_latency"`
}

type TelemetryErrorDigest struct {
	SSRF    int `json:"ssrf"`
	Timeout int `json:"timeout"`
	DNS     int `json:"dns"`
	HTTP5xx int `json:"http5xx"`
	HTTP4xx int `json:"http4xx"`
	Others  int `json:"others"`
}

type TelemetryResponse struct {
	Buckets []*TelemetryBucket    `json:"buckets"`
	Summary TelemetrySummary      `json:"summary"`
	Errors  TelemetryErrorDigest  `json:"errors"`
}

// GetTelemetryData returns mathematically aggregated telemetry buckets for any project and arbitrary time range.
func (r *ExecutionRepository) GetTelemetryData(
	ctx context.Context,
	projectID string,
	startTime time.Time,
	intervalSeconds int,
	jobIDs []string,
) (*TelemetryResponse, error) {
	if intervalSeconds <= 0 {
		intervalSeconds = 3600
	}

	whereClause := "WHERE j.project_id = $1 AND e.triggered_at >= $2"
	args := []interface{}{projectID, startTime}
	argCount := 2

	if len(jobIDs) > 0 {
		argCount++
		args = append(args, pq.Array(jobIDs))
		whereClause += fmt.Sprintf(" AND j.id = ANY($%d)", argCount)
	}

	// 1. Query Buckets
	bucketArgs := append([]interface{}{}, args...)
	argCount++
	bucketArgs = append(bucketArgs, intervalSeconds)
	intervalArg := argCount

	bucketQuery := fmt.Sprintf(`
		SELECT 
			to_timestamp(floor(extract(epoch from e.triggered_at) / $%d) * $%d) as bucket_time,
			COUNT(e.id) as volume,
			COUNT(e.id) FILTER (WHERE e.status = 'success') as success_count,
			COUNT(e.id) FILTER (WHERE e.status = 'failed') as failed_count,
			COALESCE(ROUND(AVG(e.duration_ms)), 0) as avg_latency,
			COALESCE(MAX(e.duration_ms), 0) as max_latency,
			COALESCE(ARRAY_AGG(DISTINCT j.name) FILTER (WHERE e.status = 'failed'), '{}') as failed_jobs
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		%s
		GROUP BY bucket_time
		ORDER BY bucket_time ASC`,
		intervalArg, intervalArg, whereClause,
	)

	rows, err := r.db.QueryContext(ctx, bucketQuery, bucketArgs...)
	if err != nil {
		return nil, fmt.Errorf("ExecutionRepository.GetTelemetryData buckets: %w", err)
	}
	defer rows.Close()

	var buckets []*TelemetryBucket
	for rows.Next() {
		b := &TelemetryBucket{}
		var failedJobs pq.StringArray
		if err := rows.Scan(
			&b.BucketTime,
			&b.Volume,
			&b.SuccessCount,
			&b.FailedCount,
			&b.AvgLatency,
			&b.MaxLatency,
			&failedJobs,
		); err != nil {
			return nil, fmt.Errorf("ExecutionRepository.GetTelemetryData scan bucket: %w", err)
		}
		b.FailedJobs = failedJobs
		if b.FailedJobs == nil {
			b.FailedJobs = []string{}
		}
		buckets = append(buckets, b)
	}

	if buckets == nil {
		buckets = []*TelemetryBucket{}
	}

	// 2. Query Summary
	summaryQuery := fmt.Sprintf(`
		SELECT 
			COUNT(e.id) as total_volume,
			COUNT(e.id) FILTER (WHERE e.status = 'success') as success_count,
			COUNT(e.id) FILTER (WHERE e.status = 'failed') as failed_count,
			COALESCE(ROUND(AVG(e.duration_ms)), 0) as avg_latency,
			COALESCE(MAX(e.duration_ms), 0) as max_latency
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		%s`,
		whereClause,
	)

	var summary TelemetrySummary
	err = r.db.QueryRowContext(ctx, summaryQuery, args...).Scan(
		&summary.TotalVolume,
		&summary.SuccessCount,
		&summary.FailedCount,
		&summary.AvgLatency,
		&summary.MaxLatency,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("ExecutionRepository.GetTelemetryData summary: %w", err)
	}

	if summary.TotalVolume > 0 {
		summary.SuccessRate = float64(summary.SuccessCount) / float64(summary.TotalVolume) * 100.0
	} else {
		summary.SuccessRate = 100.0
	}

	// 3. Error classification breakdown
	errQuery := fmt.Sprintf(`
		SELECT 
			COALESCE(e.response_body, '') as resp,
			COALESCE(e.http_status, 0) as status_code,
			COUNT(e.id) as cnt
		FROM executions e
		INNER JOIN jobs j ON j.id = e.job_id
		%s AND e.status = 'failed'
		GROUP BY resp, status_code`,
		whereClause,
	)

	errRows, err := r.db.QueryContext(ctx, errQuery, args...)
	var errorDigest TelemetryErrorDigest
	if err == nil {
		defer errRows.Close()
		for errRows.Next() {
			var resp string
			var status int
			var count int
			if scanErr := errRows.Scan(&resp, &status, &count); scanErr == nil {
				respLower := strings.ToLower(resp)
				if strings.Contains(respLower, "ssrf") {
					errorDigest.SSRF += count
				} else if strings.Contains(respLower, "timeout") || strings.Contains(respLower, "deadline") {
					errorDigest.Timeout += count
				} else if strings.Contains(respLower, "lookup") || strings.Contains(respLower, "dns") || strings.Contains(respLower, "no such host") {
					errorDigest.DNS += count
				} else if status >= 500 {
					errorDigest.HTTP5xx += count
				} else if status >= 400 {
					errorDigest.HTTP4xx += count
				} else {
					errorDigest.Others += count
				}
			}
		}
	}

	return &TelemetryResponse{
		Buckets: buckets,
		Summary: summary,
		Errors:  errorDigest,
	}, nil
}


