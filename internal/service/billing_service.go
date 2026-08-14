package service

import (
	"context"
	"fmt"

	"github.com/JanGustavo/Cron/internal/domain/billing"
)

type BillingRepository interface {
	GetSubscription(ctx context.Context, userID string) (*billing.Subscription, error)
	GetPlanLimits(ctx context.Context, planCode string) (*billing.PlanLimits, error)
	CountActiveJobs(ctx context.Context, userID string) (int, error)
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
