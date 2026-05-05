-- Migration: 002 — Jobs e Execuções
-- Estrutura central do produto.

CREATE TABLE jobs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    schedule             TEXT NOT NULL,         -- cron expr ou "every:15m"
    timezone             TEXT NOT NULL DEFAULT 'UTC',
    url                  TEXT NOT NULL,
    http_method          TEXT NOT NULL DEFAULT 'POST',
    headers              JSONB,
    payload              JSONB,
    status               TEXT NOT NULL DEFAULT 'active', -- active|paused|failing
    next_run_at          TIMESTAMPTZ NOT NULL,
    last_run_at          TIMESTAMPTZ,
    consecutive_failures INT NOT NULL DEFAULT 0,
    webhook_alert_url    TEXT,                  -- URL para alertas de falha
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Índice parcial crítico: o Scheduler usa essa query a cada 30s.
-- Indexar SOMENTE jobs ativos reduz ~50% do tamanho do índice.
CREATE INDEX idx_jobs_scheduler ON jobs (next_run_at ASC)
    WHERE status = 'active';

CREATE TABLE executions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    triggered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    status         TEXT NOT NULL,   -- success | failed | timeout
    http_status    INT,
    duration_ms    INT,
    response_body  TEXT,            -- truncado em 2KB pelo Worker
    attempt_number INT NOT NULL DEFAULT 1
);

-- Índice para paginação do histórico por job (DESC = mais recente primeiro)
CREATE INDEX idx_executions_job_time ON executions (job_id, triggered_at DESC);

-- Índice para o job de retenção (deletar logs antigos)
CREATE INDEX idx_executions_retention ON executions (triggered_at ASC);
