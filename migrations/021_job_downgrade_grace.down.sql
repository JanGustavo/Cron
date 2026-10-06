DROP TRIGGER IF EXISTS subscription_account_policy ON subscriptions;
DROP FUNCTION IF EXISTS cronflow_subscription_changed();
DROP FUNCTION IF EXISTS cronflow_reconcile_account(UUID,TIMESTAMPTZ);
DROP FUNCTION IF EXISTS cronflow_effective_plan(UUID,TIMESTAMPTZ);
ALTER TABLE jobs DROP COLUMN IF EXISTS pause_reason;
ALTER TABLE users DROP COLUMN IF EXISTS job_grace_ends_at;
ALTER TABLE users DROP COLUMN IF EXISTS job_grace_active_cap;
ALTER TABLE users DROP COLUMN IF EXISTS pro_ended_at;
