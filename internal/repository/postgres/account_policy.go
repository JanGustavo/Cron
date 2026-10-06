package postgres

import (
	"context"
	"github.com/JanGustavo/Cron/internal/domain/billing"
)

func (r *BillingRepository) GetJobGrace(ctx context.Context, userID string) (*billing.JobGrace, error) {
	if _, err := r.db.ExecContext(ctx, `SELECT cronflow_reconcile_account($1::uuid)`, userID); err != nil {
		return nil, err
	}
	grace := &billing.JobGrace{}
	err := r.db.QueryRowContext(ctx, `SELECT u.job_grace_ends_at,(SELECT COUNT(*) FROM jobs j JOIN projects p ON p.id=j.project_id WHERE p.user_id=u.id AND j.status='active'),p.max_jobs,u.pro_ended_at IS NOT NULL FROM users u CROSS JOIN plans p WHERE u.id=$1 AND p.code='free'`, userID).Scan(&grace.EndsAt, &grace.ActiveJobs, &grace.MaxJobs, &grace.ProEnded)
	return grace, err
}
func (r *JobRepository) ExecutionLimits(ctx context.Context, projectID string) (*billing.PlanLimits, error) {
	var uid string
	if err := r.db.QueryRowContext(ctx, `SELECT user_id FROM projects WHERE id=$1`, projectID).Scan(&uid); err != nil {
		return nil, err
	}
	br := NewBillingRepository(r.db)
	if _, err := br.GetSubscription(ctx, uid); err != nil {
		return nil, err
	}
	var code string
	if err := r.db.QueryRowContext(ctx, `SELECT cronflow_effective_plan($1::uuid)`, uid).Scan(&code); err != nil {
		return nil, err
	}
	return br.GetPlanLimits(ctx, code)
}
