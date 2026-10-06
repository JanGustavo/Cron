-- Older clients do not understand validation_failed; retain its evidence in JSONB.
UPDATE executions SET status = COALESCE(transport_status, 'success') WHERE status = 'validation_failed';
ALTER TABLE executions DROP COLUMN transport_status;
ALTER TABLE monitor_rules DROP CONSTRAINT monitor_rules_success_job_check,
    DROP CONSTRAINT monitor_rules_value_type_check,
    DROP CONSTRAINT monitor_rules_mode_check,
    DROP COLUMN mode, DROP COLUMN value_type;
