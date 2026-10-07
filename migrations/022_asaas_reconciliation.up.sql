CREATE TABLE billing_checkouts (
 provider_link_id TEXT PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 cycle TEXT CHECK(cycle IN ('MONTHLY','YEARLY')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX billing_checkouts_user ON billing_checkouts(user_id);
-- Preserve legacy links; their cycle will be read from Asaas, never guessed by price.
INSERT INTO billing_checkouts(provider_link_id,user_id)
 SELECT provider_subscription_id,user_id FROM subscriptions
 WHERE billing_provider='asaas' AND provider_subscription_id IS NOT NULL AND provider_subscription_id NOT LIKE 'sub\_%' ESCAPE '\'
 ON CONFLICT DO NOTHING;
ALTER TABLE subscriptions ADD COLUMN provider_checked_at TIMESTAMPTZ;
ALTER TABLE subscriptions ADD COLUMN provider_cycle TEXT CHECK(provider_cycle IN ('MONTHLY','YEARLY'));
ALTER TABLE billing_events ADD COLUMN processing_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE billing_events ADD COLUMN next_retry_at TIMESTAMPTZ;
CREATE INDEX billing_events_retry ON billing_events(created_at) WHERE processed_at IS NULL OR processing_error IS NOT NULL;
