package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/domain/job"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

// Diagnostic audit, opt-in and disposable DB only. Findings are observations,
// not assertions that the present downgrade policy is correct.
func TestBillingLifecycleAudit(t *testing.T) {
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
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var uid, pid string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,plan) VALUES (gen_random_uuid()::text||'@example.invalid','test','free') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, uid)
	if err := db.QueryRowContext(ctx, `INSERT INTO projects(user_id,name) VALUES ($1,'Billing audit') RETURNING id`, uid).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	br := postgres.NewBillingRepository(db)
	engine := service.NewEntitlementEngine(br)
	jr := postgres.NewJobRepository(db)
	ur := postgres.NewUserRepository(db)
	js := service.NewJobService(jr, ur, engine, nil, &config.Config{})
	providerID := "sub_audit_" + uid
	sub := &billing.Subscription{UserID: uid, PlanCode: "pro", Status: "pending", BillingProvider: "asaas", ProviderSubscriptionID: &providerID}
	saveSub := func() {
		t.Helper()
		if err := br.UpsertSubscription(ctx, sub); err != nil {
			t.Fatal(err)
		}
	}
	saveSub()
	observations := map[string]any{}
	snapshot := func(name string) {
		t.Helper()
		limits, err := engine.GetUserLimits(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		var stored, active int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE status='active') FROM jobs WHERE project_id=$1`, pid).Scan(&stored, &active); err != nil {
			t.Fatal(err)
		}
		state := map[string]any{"effective_plan": limits.Code, "max_jobs": limits.MaxJobs, "stored_jobs": stored, "active_jobs": active}
		observations[name] = state
		t.Logf("%s: %+v", name, state)
	}
	snapshot("01_pending_checkout")
	h := handler.NewBillingHandler(engine, &config.Config{AsaasWebhookToken: "audit-only-token"})
	send := func(event, paymentID string) *httptest.ResponseRecorder {
		t.Helper()
		payload := handler.AsaasWebhookPayload{Event: event}
		if event == "SUBSCRIPTION_DELETED" {
			payload.Subscription = &handler.AsaasSubscription{ID: providerID}
		} else {
			payload.Payment = &handler.AsaasPayment{ID: paymentID, Subscription: providerID, ExternalReference: uid, Customer: "audit_customer", Value: 29}
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/billing/webhook", nil)
		h.ProcessAsaasWebhook(rec, req, raw, "audit-only-token")
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", event, rec.Code, rec.Body.String())
		}
		return rec
	}
	send("PAYMENT_CONFIRMED", "pay_upgrade_"+uid)
	snapshot("02_confirmed_upgrade")
	ids := make([]string, 8)
	for i := range ids {
		if err := db.QueryRowContext(ctx, `INSERT INTO jobs(project_id,name,schedule,url,next_run_at,created_at) VALUES ($1,$2,'0 0 * * *','https://example.invalid',NOW()-INTERVAL '1 minute',NOW()-INTERVAL '1 day'+make_interval(mins=>$3::int)) RETURNING id`, pid, "Audit job", i).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Preserve paid settings to examine whether downgrade removes them.
	mustExec(`UPDATE jobs SET next_job_id=$1,webhook_alert_url='https://example.invalid/hook' WHERE id=$2`, ids[1], ids[0])
	for _, event := range []string{"PAYMENT_OVERDUE", "PAYMENT_REFUNDED", "PAYMENT_CHARGEBACK_REQUESTED", "SUBSCRIPTION_DELETED"} {
		sub.PlanCode = "pro"
		sub.Status = "active"
		saveSub()
		send(event, "pay_"+event+uid)
		snapshot("03_" + event)
	}
	eligible, err := jr.FindEligibleToRun(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ownEligible := 0
	for _, j := range eligible {
		if j.ProjectID == pid {
			ownEligible++
		}
	}
	observations["04_scheduler_after_automatic_downgrade"] = ownEligible
	storedJob, err := jr.FindByID(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	observations["05_paid_settings_preserved"] = map[string]bool{"workflow": storedJob.NextJobID != nil, "webhook": storedJob.WebhookAlertURL != nil}
	if err := ur.UpdateUserPlanByAdmin(ctx, uid, "free"); err != nil {
		t.Fatal(err)
	}
	snapshot("06_admin_downgrade")
	var activeOrdinals, pausedOrdinals []int
	for i, id := range ids {
		j, err := jr.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == job.StatusActive {
			activeOrdinals = append(activeOrdinals, i+1)
		} else {
			pausedOrdinals = append(pausedOrdinals, i+1)
		}
	}
	observations["07_admin_selection"] = map[string]any{"active_oldest_ordinals": activeOrdinals, "paused_newest_ordinals": pausedOrdinals}
	for _, id := range ids[5:] {
		if err := js.UpdateStatus(ctx, id, pid, job.StatusActive); err != nil {
			t.Fatal(err)
		}
	}
	snapshot("08_free_reactivation")
	observations["09_new_job_creation_denied"] = engine.CheckJobCreation(ctx, uid) != nil
	past := time.Now().Add(-24 * time.Hour)
	sub.PlanCode = "pro"
	sub.Status = "active"
	sub.CurrentPeriodEnd = &past
	sub.CancelAtPeriodEnd = true
	saveSub()
	snapshot("10_expired_pro")
	future := time.Now().Add(24 * time.Hour)
	sub.CurrentPeriodEnd = &future
	saveSub()
	snapshot("11_cancel_at_future_period_end")
	// Failed registered events are currently recognized as duplicates on retry.
	eventID := "audit_failed_" + uid
	event, _, err := engine.RegisterBillingEvent(ctx, "asaas", eventID, "PAYMENT_CONFIRMED", nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	failure := "simulated transient database error"
	if err := engine.MarkEventProcessed(ctx, event.ID, &failure); err != nil {
		t.Fatal(err)
	}
	_, duplicate, err := engine.RegisterBillingEvent(ctx, "asaas", eventID, "PAYMENT_CONFIRMED", nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	observations["12_failed_event_retry_ignored"] = duplicate
	// A delayed event for an older payment can restore Pro after a newer refund.
	send("PAYMENT_REFUNDED", "pay_new_refund_"+uid)
	send("PAYMENT_RECEIVED", "pay_old_received_"+uid)
	snapshot("13_old_payment_after_new_refund")
	t.Logf("scheduler eligible after downgrade: %d; admin active: %v; paused: %v; failed retry ignored: %v", ownEligible, activeOrdinals, pausedOrdinals, duplicate)
	if output := os.Getenv("CRONFLOW_BILLING_AUDIT_OUTPUT"); output != "" {
		raw, err := json.MarshalIndent(observations, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
