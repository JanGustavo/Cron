DROP INDEX IF EXISTS billing_events_retry;
ALTER TABLE billing_events DROP COLUMN IF EXISTS processing_attempts;
ALTER TABLE billing_events DROP COLUMN IF EXISTS next_retry_at;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS provider_checked_at;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS provider_cycle;
DROP TABLE IF EXISTS billing_checkouts;
