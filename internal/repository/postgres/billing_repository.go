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
			var userPlan string
			_ = r.db.QueryRowContext(ctx, `SELECT COALESCE(plan, 'free') FROM users WHERE id = $1`, userID).Scan(&userPlan)
			if userPlan == "pro" {
				return &billing.Subscription{
					UserID:   userID,
					PlanCode: "pro",
					Status:   "active",
				}, nil
			}
			return nil, nil // No subscription is fine, free plan applies
		}
		return nil, err
	}

	var userPlan string
	_ = r.db.QueryRowContext(ctx, `SELECT COALESCE(plan, 'free') FROM users WHERE id = $1`, userID).Scan(&userPlan)
	if userPlan == "pro" {
		sub.PlanCode = "pro"
		sub.Status = "active"
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

// CreateBillingEvent inserts a new billing event record in the database.
func (r *BillingRepository) CreateBillingEvent(ctx context.Context, event *billing.BillingEvent) error {
	query := `
		INSERT INTO billing_events (provider, provider_event_id, event_type, user_id, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var userIDVal sql.NullString
	if event.UserID != nil {
		userIDVal = sql.NullString{String: *event.UserID, Valid: true}
	}

	return r.db.QueryRowContext(ctx, query,
		event.Provider,
		event.ProviderEventID,
		event.EventType,
		userIDVal,
		event.Payload,
		event.CreatedAt,
	).Scan(&event.ID)
}

// FindBillingEventByProviderID checks if a billing event has already been registered in the database.
func (r *BillingRepository) FindBillingEventByProviderID(ctx context.Context, provider, providerEventID string) (*billing.BillingEvent, error) {
	query := `
		SELECT id, provider, provider_event_id, event_type, user_id, payload, processed_at, processing_error, created_at
		FROM billing_events
		WHERE provider = $1 AND provider_event_id = $2
		LIMIT 1
	`
	event := &billing.BillingEvent{}
	var userIDVal, errVal sql.NullString
	var procTime sql.NullTime

	err := r.db.QueryRowContext(ctx, query, provider, providerEventID).Scan(
		&event.ID, &event.Provider, &event.ProviderEventID, &event.EventType,
		&userIDVal, &event.Payload, &procTime, &errVal, &event.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if userIDVal.Valid {
		event.UserID = &userIDVal.String
	}
	if errVal.Valid {
		event.ProcessingError = &errVal.String
	}
	if procTime.Valid {
		event.ProcessedAt = &procTime.Time
	}

	return event, nil
}

// MarkBillingEventProcessed updates an event record's processed_at time and optional processing error.
func (r *BillingRepository) MarkBillingEventProcessed(ctx context.Context, id string, errStr *string) error {
	query := `
		UPDATE billing_events
		SET processed_at = NOW(), processing_error = $2
		WHERE id = $1
	`
	var errVal sql.NullString
	if errStr != nil {
		errVal = sql.NullString{String: *errStr, Valid: true}
	}

	_, err := r.db.ExecContext(ctx, query, id, errVal)
	return err
}

// UpsertSubscription inserts or updates a subscription record in the database.
func (r *BillingRepository) UpsertSubscription(ctx context.Context, sub *billing.Subscription) error {
	query := `
		INSERT INTO subscriptions (
			user_id, plan_code, status, billing_provider, 
			provider_customer_id, provider_subscription_id, 
			current_period_start, current_period_end, cancel_at_period_end, 
			updated_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW()
		)
		ON CONFLICT (user_id) DO UPDATE SET
			plan_code = EXCLUDED.plan_code,
			status = EXCLUDED.status,
			billing_provider = EXCLUDED.billing_provider,
			provider_customer_id = EXCLUDED.provider_customer_id,
			provider_subscription_id = EXCLUDED.provider_subscription_id,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			cancel_at_period_end = EXCLUDED.cancel_at_period_end,
			updated_at = NOW()
		RETURNING id
	`
	var custID, subID sql.NullString
	if sub.ProviderCustomerID != nil {
		custID = sql.NullString{String: *sub.ProviderCustomerID, Valid: true}
	}
	if sub.ProviderSubscriptionID != nil {
		subID = sql.NullString{String: *sub.ProviderSubscriptionID, Valid: true}
	}
	var pStart, pEnd sql.NullTime
	if sub.CurrentPeriodStart != nil {
		pStart = sql.NullTime{Time: *sub.CurrentPeriodStart, Valid: true}
	}
	if sub.CurrentPeriodEnd != nil {
		pEnd = sql.NullTime{Time: *sub.CurrentPeriodEnd, Valid: true}
	}

	err := r.db.QueryRowContext(ctx, query,
		sub.UserID,
		sub.PlanCode,
		sub.Status,
		sub.BillingProvider,
		custID,
		subID,
		pStart,
		pEnd,
		sub.CancelAtPeriodEnd,
	).Scan(&sub.ID)
	if err != nil {
		return err
	}

	// Sincroniza a coluna 'plan' do usuário de acordo com o status da assinatura
	userPlan := "free"
	if sub.Status == "active" || sub.Status == "trialing" {
		userPlan = sub.PlanCode
	}

	_, err = r.db.ExecContext(ctx, "UPDATE users SET plan = $1 WHERE id = $2", userPlan, sub.UserID)
	return err
}

// GetSubscriptionByProviderSubID retrieves a subscription record using the Stripe subscription ID.
func (r *BillingRepository) GetSubscriptionByProviderSubID(ctx context.Context, providerSubID string) (*billing.Subscription, error) {
	query := `
		SELECT id, user_id, plan_code, status, billing_provider, 
		       provider_customer_id, provider_subscription_id, 
		       current_period_start, current_period_end, cancel_at_period_end, 
		       updated_at, created_at
		FROM subscriptions
		WHERE provider_subscription_id = $1 LIMIT 1
	`
	sub := &billing.Subscription{}
	var custID, subID sql.NullString
	var pStart, pEnd sql.NullTime

	err := r.db.QueryRowContext(ctx, query, providerSubID).Scan(
		&sub.ID, &sub.UserID, &sub.PlanCode, &sub.Status, &sub.BillingProvider,
		&custID, &subID, &pStart, &pEnd, &sub.CancelAtPeriodEnd,
		&sub.UpdatedAt, &sub.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
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
