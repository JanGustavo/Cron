-- Migration: 015 — Regras de Monitoramento e Leitura de Chaves
CREATE TABLE IF NOT EXISTS monitor_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    key VARCHAR(100) NOT NULL,
    operator VARCHAR(10) NOT NULL, -- 'gt', 'gte', 'lt', 'lte', 'eq', 'ne', 'contains'
    threshold_value TEXT NOT NULL,
    alert_email BOOLEAN NOT NULL DEFAULT TRUE,
    webhook_url TEXT,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_monitor_rules_project_key ON monitor_rules(project_id, key);

CREATE TABLE IF NOT EXISTS monitor_key_values (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key VARCHAR(100) NOT NULL,
    current_value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_project_key UNIQUE (project_id, key)
);
