CREATE TABLE IF NOT EXISTS plans (
    code VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    price_monthly INT NOT NULL DEFAULT 0,
    price_yearly INT NOT NULL DEFAULT 0,
    max_jobs INT NOT NULL DEFAULT 5,
    max_users INT NOT NULL DEFAULT 1,
    logs_retention_days INT NOT NULL DEFAULT 7,
    workflows_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    alerts_webhooks_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    multi_project_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_code VARCHAR(50) NOT NULL REFERENCES plans(code),
    status VARCHAR(30) NOT NULL DEFAULT 'trialing',
    
    billing_provider VARCHAR(50) NOT NULL DEFAULT 'stripe',
    provider_customer_id VARCHAR(100),
    provider_subscription_id VARCHAR(100),
    
    current_period_start TIMESTAMP WITH TIME ZONE,
    current_period_end TIMESTAMP WITH TIME ZONE,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_user ON subscriptions(user_id);

INSERT INTO plans (code, name, price_monthly, price_yearly, max_jobs, max_users, logs_retention_days, workflows_enabled, alerts_webhooks_enabled, multi_project_enabled)
VALUES 
('free', 'Plano Free', 0, 0, 5, 1, 7, FALSE, FALSE, FALSE),
('pro', 'Plano Pro', 2900, 29000, 50, 3, 90, TRUE, TRUE, TRUE)
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    price_monthly = EXCLUDED.price_monthly,
    price_yearly = EXCLUDED.price_yearly,
    max_jobs = EXCLUDED.max_jobs,
    max_users = EXCLUDED.max_users,
    logs_retention_days = EXCLUDED.logs_retention_days,
    workflows_enabled = EXCLUDED.workflows_enabled,
    alerts_webhooks_enabled = EXCLUDED.alerts_webhooks_enabled,
    multi_project_enabled = EXCLUDED.multi_project_enabled;
