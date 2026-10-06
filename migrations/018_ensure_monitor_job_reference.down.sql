-- Remove only the repair constraint; retain the original migration 016 constraint.
ALTER TABLE monitor_rules DROP CONSTRAINT IF EXISTS monitor_rules_job_reference_fkey;
