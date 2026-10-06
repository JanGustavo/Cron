package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

func TestJobDowngradeGraceIntegration(t *testing.T) {
	dsn := os.Getenv("CRONFLOW_MONITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable migrated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	br := postgres.NewBillingRepository(db)
	ur := postgres.NewUserRepository(db)
	jr := postgres.NewJobRepository(db)
	engine := service.NewEntitlementEngine(br)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, origin := range []string{"admin", "provider", "expiry"} {
		t.Run(origin, func(t *testing.T) {
			var uid string
			if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES(gen_random_uuid()::text||'@example.invalid','test') RETURNING id`).Scan(&uid); err != nil {
				t.Fatal(err)
			}
			defer db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, uid)
			ids := []string{}
			pids := []string{}
			for project := 0; project < 2; project++ {
				var pid string
				if err := db.QueryRowContext(ctx, `INSERT INTO projects(user_id,name) VALUES($1,'Grace test') RETURNING id`, uid).Scan(&pid); err != nil {
					t.Fatal(err)
				}
				pids = append(pids, pid)
				for i := 0; i < 4; i++ {
					var id string
					if err := db.QueryRowContext(ctx, `INSERT INTO jobs(project_id,name,schedule,url,next_run_at,created_at) VALUES($1,'Test','0 0 * * *','https://example.invalid',NOW()-INTERVAL '1 minute',NOW()-INTERVAL '1 day'+make_interval(mins=>$2::int)) RETURNING id`, pid, len(ids)).Scan(&id); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				}
			}
			future := time.Now().Add(24 * time.Hour)
			sub := &billing.Subscription{UserID: uid, PlanCode: "pro", Status: "active", BillingProvider: "asaas", CurrentPeriodEnd: &future}
			if err := br.UpsertSubscription(ctx, sub); err != nil {
				t.Fatal(err)
			}
			switch origin {
			case "admin":
				if err := ur.UpdateUserPlanByAdmin(ctx, uid, "free"); err != nil {
					t.Fatal(err)
				}
			case "provider":
				sub.Status = "past_due"
				if err := br.UpsertSubscription(ctx, sub); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				past := time.Now().Add(-time.Hour)
				sub.CurrentPeriodEnd = &past
				if err := br.UpsertSubscription(ctx, sub); err != nil {
					t.Fatal(err)
				}
			}
			grace, err := br.GetJobGrace(ctx, uid)
			if err != nil {
				t.Fatal(err)
			}
			if grace.EndsAt == nil || grace.ActiveJobs != 8 || !grace.ProEnded {
				t.Fatalf("missing grace: %+v", grace)
			}
			if left := time.Until(*grace.EndsAt); left < 47*time.Hour+59*time.Minute || left > 48*time.Hour {
				t.Fatalf("invalid window: %s", left)
			}
			deadline := *grace.EndsAt
			limits, err := engine.GetUserLimits(ctx, uid)
			if err != nil || limits.Code != "free" || limits.WorkflowsEnabled || limits.AlertsWebhooksEnabled {
				t.Fatalf("grace leaked paid features: %+v %v", limits, err)
			}
			for repeat := 0; repeat < 3; repeat++ {
				g, err := br.GetJobGrace(ctx, uid)
				if err != nil || !g.EndsAt.Equal(deadline) {
					t.Fatalf("deadline reset: %+v %v", g, err)
				}
			}
			// A customer chooses the remaining five by pausing the oldest three.
			for _, id := range ids[:3] {
				if err := jr.UpdateStatus(ctx, id, job.StatusPaused); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(ctx, `SELECT cronflow_reconcile_account($1::uuid,$2)`, uid, deadline.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			for _, id := range ids[3:] {
				j, err := jr.FindByID(ctx, id)
				if err != nil || j.Status != job.StatusActive {
					t.Fatalf("customer choice lost: %v %v", j, err)
				}
			}
			// Recreate overage without changing its deadline to test automatic selection.
			exec(`UPDATE jobs SET status='active' WHERE id=ANY($1::uuid[])`, "{"+ids[0]+","+ids[1]+","+ids[2]+"}")
			if _, err := db.ExecContext(ctx, `SELECT cronflow_reconcile_account($1::uuid,$2)`, uid, deadline); err != nil {
				t.Fatal(err)
			}
			for i, id := range ids {
				j, err := jr.FindByID(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				want := job.StatusActive
				if i >= 5 {
					want = job.StatusPaused
				}
				if j.Status != want {
					t.Fatalf("job %d: %s expected %s", i, j.Status, want)
				}
			}
			if err := jr.ResetFailures(ctx, ids[7]); err != nil {
				t.Fatal(err)
			}
			paused, err := jr.FindByID(ctx, ids[7])
			if err != nil || paused.Status != job.StatusPaused {
				t.Fatal("in-flight success resumed a plan-paused job")
			}
			// Use a past persisted deadline, so real-time service checks enforce Free.
			exec(`UPDATE users SET job_grace_ends_at=NOW()-INTERVAL '1 second' WHERE id=$1`, uid)
			if err := jr.ReactivateJob(ctx, ids[7], time.Now().Add(time.Hour)); err == nil {
				t.Fatal("reactivation bypassed quota")
			}
			// A paused slot can be replaced; concurrent reactivations cannot claim it twice.
			if err := jr.UpdateStatus(ctx, ids[0], job.StatusPaused); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			results := make(chan error, 3)
			for _, id := range ids[5:] {
				wg.Add(1)
				go func(id string) { defer wg.Done(); results <- jr.ReactivateJob(ctx, id, time.Now().Add(time.Hour)) }(id)
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				}
			}
			if success != 1 {
				t.Fatalf("concurrent quota: %d activations", success)
			}
			renewed := time.Now().Add(30 * 24 * time.Hour)
			sub.PlanCode = "pro"
			sub.Status = "active"
			sub.CurrentPeriodEnd = &renewed
			if err := br.UpsertSubscription(ctx, sub); err != nil {
				t.Fatal(err)
			}
			grace, err = br.GetJobGrace(ctx, uid)
			if err != nil || grace.EndsAt != nil || grace.ProEnded || grace.ActiveJobs != 5 {
				t.Fatalf("renewal did not clear grace or resumed jobs unexpectedly: %+v %v", grace, err)
			}
			if err := jr.ReactivateJob(ctx, ids[0], time.Now().Add(time.Hour)); err != nil {
				t.Fatalf("upgrade did not release quota: %v", err)
			}
		})
	}
}
