-- name: CreateExecution :one
INSERT INTO executions (job_id, status, http_status, duration_ms, response_body, attempt_number)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListExecutionsByJob :many
SELECT * FROM executions
WHERE job_id = $1
ORDER BY triggered_at DESC
LIMIT $2;

-- name: DeleteOldExecutions :exec
-- Job de retenção: roda diariamente para limpar logs antigos.
DELETE FROM executions WHERE triggered_at < NOW() - ($1 || ' days')::INTERVAL;
