-- Migration: 016 — Vincular Regras de Monitoramento a Jobs e Execuções
ALTER TABLE monitor_rules ADD COLUMN IF NOT EXISTS job_id UUID REFERENCES jobs(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_monitor_rules_job_id ON monitor_rules(job_id);

ALTER TABLE executions ADD COLUMN IF NOT EXISTS rule_evaluations JSONB;
