ALTER TABLE executions DROP COLUMN IF EXISTS rule_evaluations;
ALTER TABLE monitor_rules DROP COLUMN IF EXISTS job_id;
