package billing

import "time"

type PlanLimits struct {
	Code                  string    `json:"code"`
	Name                  string    `json:"name"`
	PriceMonthly          int       `json:"price_monthly"`
	PriceYearly           int       `json:"price_yearly"`
	MaxJobs               int       `json:"max_jobs"`
	MaxUsers              int       `json:"max_users"`
	LogsRetentionDays     int       `json:"logs_retention_days"`
	WorkflowsEnabled      bool      `json:"workflows_enabled"`
	AlertsWebhooksEnabled bool      `json:"alerts_webhooks_enabled"`
	MultiProjectEnabled   bool      `json:"multi_project_enabled"`
	CreatedAt             time.Time `json:"created_at"`
}

type Subscription struct {
	ID                     string     `json:"id"`
	UserID                 string     `json:"user_id"`
	PlanCode               string     `json:"plan_code"`
	Status                 string     `json:"status"`
	BillingProvider        string     `json:"billing_provider"`
	ProviderCustomerID     *string    `json:"provider_customer_id,omitempty"`
	ProviderSubscriptionID *string    `json:"provider_subscription_id,omitempty"`
	CurrentPeriodStart     *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd       *time.Time `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd      bool       `json:"cancel_at_period_end"`
	UpdatedAt              time.Time  `json:"updated_at"`
	CreatedAt              time.Time  `json:"created_at"`
}
