-- name: CreateJob :one
INSERT INTO jobs (project_id, name, schedule, timezone, url, http_method, headers, payload, next_run_at, webhook_alert_url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetJobByID :one
SELECT * FROM jobs WHERE id = $1 LIMIT 1;

-- name: ListJobsByProject :many
SELECT * FROM jobs WHERE project_id = $1 ORDER BY created_at DESC;

-- name: FindEligibleJobs :many
-- Query crítica do Scheduler. Usa o índice parcial idx_jobs_scheduler.
-- LIMIT 500 previne que um burst enorme trave o Scheduler por tempo demais.
SELECT * FROM jobs
WHERE status = 'active'
  AND next_run_at <= $1
ORDER BY next_run_at ASC
LIMIT 500;

-- name: UpdateJobNextRun :exec
UPDATE jobs
SET next_run_at = $2, last_run_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: UpdateJobStatus :exec
UPDATE jobs SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: IncrementJobFailures :exec
UPDATE jobs
SET consecutive_failures = consecutive_failures + 1,
    status = CASE WHEN consecutive_failures + 1 >= 3 THEN 'failing' ELSE status END,
    updated_at = NOW()
WHERE id = $1;

-- name: ResetJobFailures :exec
UPDATE jobs SET consecutive_failures = 0, status = 'active', updated_at = NOW()
WHERE id = $1;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE id = $1 AND project_id = $2;

-- name: CountJobsByProject :one
SELECT COUNT(*) FROM jobs WHERE project_id = $1 AND status != 'paused';
