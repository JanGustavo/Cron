package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

func TestLogRetentionMatchesAccountEntitlements(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO plans(code,name,logs_retention_days) VALUES ('retention-test','Retention test',14)`); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM plans WHERE code='retention-test'`)
	engine := service.NewEntitlementEngine(postgres.NewBillingRepository(db))
	cases := []struct {
		name, userPlan, subscriptionPlan, status, expectedPlan string
		days                                                   int
	}{
		{"Free without subscription", "free", "", "", "free", 7},
		{"Free pending subscription", "free", "pro", "pending", "free", 7},
		{"Free canceled subscription", "free", "pro", "canceled", "free", 7},
		{"Paid active subscription", "free", "pro", "active", "pro", 90},
		{"Paid trial subscription", "free", "pro", "trialing", "pro", 90},
		{"Pro without subscription", "pro", "", "", "pro", 90},
		{"Canceled subscription overrides stale Pro mirror", "pro", "pro", "canceled", "free", 7},
		{"Dynamic plan retention", "free", "retention-test", "active", "retention-test", 14},
	}
	var freshIDs []string
	for _, tc := range cases {
		var userID, projectID, jobID string
		if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,plan) VALUES (gen_random_uuid()::text||'@example.invalid','test',$1) RETURNING id`, tc.userPlan).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		defer db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
		if tc.subscriptionPlan != "" {
			if _, err := db.ExecContext(ctx, `INSERT INTO subscriptions(user_id,plan_code,status) VALUES ($1,$2,$3)`, userID, tc.subscriptionPlan, tc.status); err != nil {
				t.Fatal(err)
			}
		}
		limits, err := engine.GetUserLimits(ctx, userID)
		if err != nil || limits.Code != tc.expectedPlan || limits.LogsRetentionDays != tc.days {
			t.Fatalf("%s entitlement mismatch: %+v %v", tc.name, limits, err)
		}
		if err := db.QueryRowContext(ctx, `INSERT INTO projects(user_id,name) VALUES ($1,'Retention test') RETURNING id`, userID).Scan(&projectID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `INSERT INTO jobs(project_id,name,schedule,url,next_run_at) VALUES ($1,'Retention test','0 0 1 1 *','https://example.invalid',NOW()+INTERVAL '1 year') RETURNING id`, projectID).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		for _, hours := range []int{-1, 1} {
			var executionID string
			if err := db.QueryRowContext(ctx, `INSERT INTO executions(job_id,status,triggered_at) VALUES ($1,'success',NOW()-make_interval(days=>$2::int)+make_interval(hours=>$3::int)) RETURNING id`, jobID, tc.days, hours).Scan(&executionID); err != nil {
				t.Fatal(err)
			}
			if hours == 1 {
				freshIDs = append(freshIDs, executionID)
			}
		}
	}
	removed, err := postgres.NewExecutionRepository(db).DeleteExpiredExecutions(ctx)
	if err != nil || removed != int64(len(cases)) {
		t.Fatalf("unexpected expired count: %d %v", removed, err)
	}
	for index, id := range freshIDs {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE id=$1`, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s lost a log within its retention window: %v", cases[index].name, err)
		}
	}
}
