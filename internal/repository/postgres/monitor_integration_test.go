package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/JanGustavo/Cron/internal/domain/execution"
	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

// Run only against an explicitly supplied disposable, migrated test database.
func TestMonitorPersistenceIntegration(t *testing.T) {
	dsn := os.Getenv("CRONFLOW_MONITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL via CRONFLOW_MONITOR_TEST_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var userID, projectID, jobID string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES (gen_random_uuid()::text || '@example.invalid','test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
	if err := db.QueryRowContext(ctx, `INSERT INTO projects(user_id,name) VALUES ($1,'Monitoring test') RETURNING id`, userID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO jobs(project_id,name,schedule,url,next_run_at) VALUES ($1,'Sync','* * * * *','https://example.invalid',NOW()) RETURNING id`, projectID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	repo := NewMonitorRepository(db)
	rule, err := repo.CreateRule(ctx, &monitor.MonitorRule{ProjectID: projectID, JobID: &jobID, Name: "Sync", Key: "/synced", Operator: monitor.OpEq, ThresholdValue: "true", Mode: monitor.ModeRequireSuccess, ValueType: "boolean", IsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	rule.Name = "Updated"
	rule, err = repo.UpdateRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	for _, load := range []func() ([]*monitor.MonitorRule, error){func() ([]*monitor.MonitorRule, error) { return repo.ListRulesByJob(ctx, projectID, jobID) }, func() ([]*monitor.MonitorRule, error) { return repo.ListRulesByProject(ctx, projectID) }} {
		rules, err := load()
		if err != nil || len(rules) != 1 || rules[0].Mode != monitor.ModeRequireSuccess || rules[0].ValueType != "boolean" || rules[0].Name != "Updated" {
			t.Fatalf("rule persistence mismatch: %+v %v", rules, err)
		}
	}
	global, err := repo.ListRulesByKey(ctx, projectID, "/synced")
	if err != nil || len(global) != 0 {
		t.Fatalf("job condition leaked globally: %+v %v", global, err)
	}
	history := NewExecutionRepository(db)
	code := 200
	evaluated := monitor.EvaluateResponse([]*monitor.MonitorRule{rule}, monitor.Response{Received: true, Body: []byte(`{"synced":false}`)})
	entry := &execution.Execution{JobID: jobID, Status: execution.StatusValidationFailed, TransportStatus: execution.StatusSuccess, HTTPStatus: &code, AttemptNumber: 1, ResponseBody: `{"synced":false}`}
	if err := history.CreateWithRuleEvaluations(ctx, entry, evaluated); err != nil {
		t.Fatal(err)
	}
	logs, err := history.ListByJob(ctx, jobID, 10)
	if err != nil || len(logs) != 1 || logs[0].Status != execution.StatusValidationFailed || logs[0].TransportStatus != execution.StatusSuccess {
		t.Fatalf("outcome persistence mismatch: %+v %v", logs, err)
	}
	project, total, err := history.ListByProject(ctx, projectID, 10, 0, "", "validation_failed", "", "")
	if err != nil || total != 1 || len(project) != 1 {
		t.Fatalf("project status filter mismatch: %+v %v", project, err)
	}
	snapshot, err := history.GetRuleEvaluations(ctx, entry.ID)
	if err != nil || len(snapshot) != 1 || snapshot[0].Mode != monitor.ModeRequireSuccess || snapshot[0].Reason != "condition_not_met" {
		t.Fatalf("snapshot mismatch: %+v %v", snapshot, err)
	}
	// A disabled success condition stays saved but does not evaluate or block execution.
	rule.IsEnabled = false
	if _, err := repo.UpdateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	active, err := repo.ListRulesByJob(ctx, projectID, jobID)
	if err != nil || len(active) != 0 {
		t.Fatalf("disabled condition evaluated: %+v %v", active, err)
	}
	if !monitor.SuccessConditionsPassed(monitor.EvaluateResponse(active, monitor.Response{Received: true, Body: []byte(`{"synced":false}`)})) {
		t.Fatal("disabled condition blocked success")
	}
	rule.IsEnabled = true
	if _, err := repo.UpdateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	active, err = repo.ListRulesByJob(ctx, projectID, jobID)
	if err != nil || len(active) != 1 || active[0].ID != rule.ID {
		t.Fatalf("reactivation lost condition: %+v %v", active, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM jobs WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	remaining, err := repo.ListRulesByProject(ctx, projectID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("job deletion left monitoring conditions: %+v %v", remaining, err)
	}

}
