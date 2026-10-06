ALTER TABLE monitor_rules
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'observe',
    ADD COLUMN value_type TEXT NOT NULL DEFAULT 'legacy',
    ADD CONSTRAINT monitor_rules_mode_check CHECK (mode IN ('observe', 'require_success')),
    ADD CONSTRAINT monitor_rules_value_type_check CHECK (value_type IN ('legacy', 'number', 'string', 'boolean', 'null')),
    ADD CONSTRAINT monitor_rules_success_job_check CHECK (mode <> 'require_success' OR job_id IS NOT NULL);

ALTER TABLE executions ADD COLUMN transport_status TEXT;
-- Old executions only recorded transport outcomes.
UPDATE executions SET transport_status = status WHERE transport_status IS NULL;
