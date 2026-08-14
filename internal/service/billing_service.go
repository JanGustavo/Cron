package service

import (
	"context"
	"fmt"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/billing"
)

type BillingRepository interface {
	GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error)
	GetPlanLimits(ctx context.Context, planCode string) (*billing.PlanLimits, error)
	CountActiveJobs(ctx context.Context, userID string) (int, error)
	CreateBillingEvent(ctx context.Context, event *billing.BillingEvent) error
	FindBillingEventByProviderID(ctx context.Context, provider, providerEventID string) (*billing.BillingEvent, error)
	MarkBillingEventProcessed(ctx context.Context, id string, errStr *string) error
	UpsertSubscription(ctx context.Context, sub *billing.Subscription) error
	GetSubscriptionByProviderSubID(ctx context.Context, providerSubID string) (*billing.Subscription, error)
}

type EntitlementEngine struct {
	repo BillingRepository
}

func NewEntitlementEngine(repo BillingRepository) *EntitlementEngine {
	return &EntitlementEngine{repo: repo}
}

// GetUserLimits returns the active plan limits for a given user.
func (e *EntitlementEngine) GetUserLimits(ctx context.Context, userID string) (*billing.PlanLimits, error) {
	sub, err := e.repo.GetSubscription(ctx, userID)
	if err != nil {
		return nil, err
	}

	planCode := "starter"
	if sub != nil && (sub.Status == "active" || sub.Status == "trialing") {
		planCode = sub.PlanCode
	}

	return e.repo.GetPlanLimits(ctx, planCode)
}

// CheckJobCreation checks if the user is allowed to create another job.
func (e *EntitlementEngine) CheckJobCreation(ctx context.Context, userID string) error {
	limits, err := e.GetUserLimits(ctx, userID)
	if err != nil {
		return err
	}

	activeJobsCount, err := e.repo.CountActiveJobs(ctx, userID)
	if err != nil {
		return err
	}

	if activeJobsCount >= limits.MaxJobs {
		return fmt.Errorf("limite de tarefas (jobs) do plano %s atingido (%d/%d)", limits.Name, activeJobsCount, limits.MaxJobs)
	}

	return nil
}

// RegisterBillingEvent registers an incoming webhook event from Stripe or Asaas, checking for duplicates.
func (e *EntitlementEngine) RegisterBillingEvent(ctx context.Context, provider, eventID, eventType string, userID *string, payload []byte) (*billing.BillingEvent, bool, error) {
	existing, err := e.repo.FindBillingEventByProviderID(ctx, provider, eventID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, true, nil
	}

	event := &billing.BillingEvent{
		Provider:        provider,
		ProviderEventID: eventID,
		EventType:       eventType,
		UserID:          userID,
		Payload:         payload,
		CreatedAt:       time.Now(),
	}

	err = e.repo.CreateBillingEvent(ctx, event)
	if err != nil {
		return nil, false, err
	}

	return event, false, nil
}

// MarkEventProcessed updates the status of the event to processed with optional error.
func (e *EntitlementEngine) MarkEventProcessed(ctx context.Context, eventID string, errStr *string) error {
	return e.repo.MarkBillingEventProcessed(ctx, eventID, errStr)
}

// UpsertSubscription inserts or updates a subscription in the database.
func (e *EntitlementEngine) UpsertSubscription(ctx context.Context, sub *billing.Subscription) error {
	return e.repo.UpsertSubscription(ctx, sub)
}

// GetSubscriptionByProviderSubID retrieves a subscription record using the Stripe subscription ID.
func (e *EntitlementEngine) GetSubscriptionByProviderSubID(ctx context.Context, providerSubID string) (*billing.Subscription, error) {
	return e.repo.GetSubscriptionByProviderSubID(ctx, providerSubID)
}

// GetSubscription retrieves subscription details for a given user.
func (e *EntitlementEngine) GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error) {
	return e.repo.GetSubscription(ctx, userID)
}
