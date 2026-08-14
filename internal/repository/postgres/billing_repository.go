package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/JanGustavo/Cron/internal/domain/billing"
)

type BillingRepository struct {
	db *sql.DB
}

func NewBillingRepository(db *sql.DB) *BillingRepository {
	return &BillingRepository{db: db}
}

// GetSubscription retrieves subscription details for a given user.
func (r *BillingRepository) GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error) {
	query := `
		SELECT id, user_id, plan_code, status, billing_provider, 
		       provider_customer_id, provider_subscription_id, 
		       current_period_start, current_period_end, cancel_at_period_end, 
		       updated_at, created_at
		FROM subscriptions
		WHERE user_id = $1 LIMIT 1
	`
	sub := &billing.Subscription{}
	var custID, subID sql.NullString
	var pStart, pEnd sql.NullTime

	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&sub.ID, &sub.UserID, &sub.PlanCode, &sub.Status, &sub.BillingProvider,
		&custID, &subID, &pStart, &pEnd, &sub.CancelAtPeriodEnd,
		&sub.UpdatedAt, &sub.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No subscription is fine, starter plan applies
		}
		return nil, err
	}

	if custID.Valid {
		sub.ProviderCustomerID = &custID.String
	}
	if subID.Valid {
		sub.ProviderSubscriptionID = &subID.String
	}
	if pStart.Valid {
		sub.CurrentPeriodStart = &pStart.Time
	}
	if pEnd.Valid {
		sub.CurrentPeriodEnd = &pEnd.Time
	}

	return sub, nil
}

// GetPlanLimits retrieves the limits configuration for a given plan code.
func (r *BillingRepository) GetPlanLimits(ctx context.Context, planCode string) (*billing.PlanLimits, error) {
	query := `
		SELECT code, name, price_monthly, price_yearly, max_jobs, max_users, 
		       logs_retention_days, workflows_enabled, alerts_webhooks_enabled, 
		       multi_project_enabled, created_at
		FROM plans
		WHERE code = $1 LIMIT 1
	`
	limits := &billing.PlanLimits{}
	err := r.db.QueryRowContext(ctx, query, planCode).Scan(
		&limits.Code, &limits.Name, &limits.PriceMonthly, &limits.PriceYearly,
		&limits.MaxJobs, &limits.MaxUsers, &limits.LogsRetentionDays,
		&limits.WorkflowsEnabled, &limits.AlertsWebhooksEnabled,
		&limits.MultiProjectEnabled, &limits.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return limits, nil
}

// CountActiveJobs counts the number of jobs created by projects owned by the user.
func (r *BillingRepository) CountActiveJobs(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(j.id)
		FROM jobs j
		JOIN projects p ON j.project_id = p.id
		WHERE p.user_id = $1
	`
	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
