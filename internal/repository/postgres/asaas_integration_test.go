package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

type auditAsaas struct {
	mu            sync.Mutex
	fail          bool
	payments      map[string]billing.AsaasPayment
	subscriptions map[string]billing.AsaasSubscription
}

func (p *auditAsaas) Payment(_ context.Context, id string) (*billing.AsaasPayment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return nil, service.AsaasHTTPError{Status: 503}
	}
	v, ok := p.payments[id]
	if !ok {
		return nil, service.AsaasHTTPError{Status: 404}
	}
	return &v, nil
}
func (p *auditAsaas) Subscription(_ context.Context, id string) (*billing.AsaasSubscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return nil, service.AsaasHTTPError{Status: 503}
	}
	v, ok := p.subscriptions[id]
	if !ok {
		return nil, service.AsaasHTTPError{Status: 404}
	}
	return &v, nil
}
func (p *auditAsaas) Link(context.Context, string) (*billing.AsaasLink, error) {
	return &billing.AsaasLink{Cycle: "MONTHLY"}, nil
}
func (p *auditAsaas) Payments(_ context.Context, uid, sub string) ([]billing.AsaasPayment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return nil, service.AsaasHTTPError{Status: 503}
	}
	out := []billing.AsaasPayment{}
	for _, v := range p.payments {
		if sub != "" && v.Subscription == sub || sub == "" && v.ExternalReference == uid {
			out = append(out, v)
		}
	}
	return out, nil
}
func (p *auditAsaas) set(v billing.AsaasPayment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.payments[v.ID] = v
}

func TestAsaasReconciliationIntegration(t *testing.T) {
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
	var uid, pid string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash) VALUES(gen_random_uuid()::text||'@example.invalid','test') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM users WHERE id=$1`, uid)
	if err := db.QueryRow(`INSERT INTO projects(user_id,name) VALUES($1,'Asaas test') RETURNING id`, uid).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewBillingRepository(db)
	engine := service.NewEntitlementEngine(repo)
	if err := repo.RecordAsaasCheckout(ctx, uid, "link-monthly", "MONTHLY"); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	date := time.Now().In(loc).Format("2006-01-02")
	current := billing.AsaasPayment{ID: "pay-current", Customer: "customer", Subscription: "sub-monthly", PaymentLink: "link-monthly", Status: "CONFIRMED", ExternalReference: uid, DueDate: date, ClientPaymentDate: date, Value: 290}
	provider := &auditAsaas{payments: map[string]billing.AsaasPayment{}, subscriptions: map[string]billing.AsaasSubscription{"sub-monthly": {ID: "sub-monthly", Cycle: "MONTHLY", Status: "ACTIVE", PaymentLink: "link-monthly"}}}
	provider.set(current)
	reconciler := service.NewAsaasReconciler(repo, provider)
	h := handler.NewBillingHandler(engine, &config.Config{AsaasWebhookToken: "local-test-token"})
	h.SetAsaasReconciler(reconciler)
	payload := func(id, event string, p billing.AsaasPayment) []byte {
		raw, err := json.Marshal(billing.AsaasEvent{ID: id, Event: event, Payment: &p})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	call := func(raw []byte) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ProcessAsaasWebhook(rec, httptest.NewRequest("POST", "/v1/billing/webhook", nil), raw, "local-test-token")
		return rec
	}
	assertPlan := func(want string) {
		t.Helper()
		limits, err := engine.GetUserLimits(ctx, uid)
		if err != nil || limits.Code != want {
			t.Fatalf("plan %+v %v expected %s", limits, err, want)
		}
	}
	if rec := call(payload("evt-confirmed", "PAYMENT_CONFIRMED", current)); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	assertPlan("pro")
	first, err := repo.GetSubscription(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	monthlyEnd := *first.CurrentPeriodEnd
	// An expensive monthly payment remains monthly; price never chooses the cycle.
	if monthlyEnd.After(time.Now().Add(40 * 24 * time.Hour)) {
		t.Fatal("price inferred yearly cycle")
	}
	current.Status = "RECEIVED"
	current.PaymentDate = time.Now().Add(32 * 24 * time.Hour).Format("2006-01-02")
	provider.set(current)
	if rec := call(payload("evt-received", "PAYMENT_RECEIVED", current)); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	settled, _ := repo.GetSubscription(ctx, uid)
	if !settled.CurrentPeriodEnd.Equal(monthlyEnd) {
		t.Fatal("settlement extended same charge")
	}
	// All duplicate deliveries have one persisted provider ID, even concurrently.
	raw := payload("evt-concurrent", "PAYMENT_RECEIVED", current)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- call(raw).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("duplicate delivery %d", code)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM billing_events WHERE provider_event_id='evt-concurrent'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate records", count, err)
	}
	// Upcoming unpaid invoice does not revoke the already paid period.
	upcoming := current
	upcoming.ID = "pay-upcoming"
	upcoming.Status = "OVERDUE"
	upcoming.ClientPaymentDate = ""
	upcoming.DueDate = time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	provider.set(upcoming)
	if rec := call(payload("evt-overdue", "PAYMENT_OVERDUE", upcoming)); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	assertPlan("pro")
	for i := 0; i < 8; i++ {
		if _, err := db.Exec(`INSERT INTO jobs(project_id,name,schedule,url,next_run_at) VALUES($1,'Test','0 0 * * *','https://example.invalid',NOW())`, pid); err != nil {
			t.Fatal(err)
		}
	}
	current.Status = "REFUNDED"
	provider.set(current)
	if rec := call(payload("evt-refund", "PAYMENT_REFUNDED", current)); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	assertPlan("free")
	grace, err := repo.GetJobGrace(ctx, uid)
	if err != nil || grace.EndsAt == nil || grace.ActiveJobs != 8 {
		t.Fatal("missing downgrade grace", grace, err)
	}
	deadline := *grace.EndsAt
	stalePayload := current
	stalePayload.Status = "RECEIVED"
	stalePayload.ClientPaymentDate = time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if rec := call(payload("evt-old-received", "PAYMENT_RECEIVED", stalePayload)); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	assertPlan("free")
	grace, _ = repo.GetJobGrace(ctx, uid)
	if !grace.EndsAt.Equal(deadline) {
		t.Fatal("old delivery reset grace")
	}
	// A new yearly renewal is verified using its own stored checkout and cycle.
	if err := repo.RecordAsaasCheckout(ctx, uid, "link-yearly", "YEARLY"); err != nil {
		t.Fatal(err)
	}
	provider.subscriptions["sub-yearly"] = billing.AsaasSubscription{ID: "sub-yearly", Cycle: "YEARLY", Status: "ACTIVE", PaymentLink: "link-yearly"}
	renewed := current
	renewed.ID = "pay-new-year"
	renewed.Subscription = "sub-yearly"
	renewed.PaymentLink = "link-yearly"
	renewed.Status = "CONFIRMED"
	renewed.ClientPaymentDate = date
	renewed.PaymentDate = ""
	renewed.Value = 29
	provider.set(renewed)
	if rec := call(payload("evt-yearly", "PAYMENT_CONFIRMED", renewed)); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	assertPlan("pro")
	annual, _ := repo.GetSubscription(ctx, uid)
	if annual.CurrentPeriodEnd.Before(time.Now().Add(350 * 24 * time.Hour)) {
		t.Fatal("discounted annual inferred monthly")
	}
	grace, _ = repo.GetJobGrace(ctx, uid)
	if grace.EndsAt != nil {
		t.Fatal("renewal retained grace")
	}
	if rec := call(payload("evt-old-refund", "PAYMENT_REFUNDED", current)); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	assertPlan("pro")
	// Provider failure retains access and persists an event for local/provider retries.
	provider.fail = true
	retryRaw := payload("evt-provider-failure", "PAYMENT_RECEIVED", renewed)
	if rec := call(retryRaw); rec.Code != 200 || !strings.Contains(rec.Body.String(), "pending_retry") {
		t.Fatal("durable failure not queued", rec.Code, rec.Body.String())
	}
	assertPlan("pro")
	failed, err := repo.FindBillingEventByProviderID(ctx, "asaas", "evt-provider-failure")
	if err != nil || failed.ProcessingError == nil {
		t.Fatal("failure not persisted", failed, err)
	}
	provider.fail = false
	if _, err := db.Exec(`UPDATE billing_events SET next_retry_at=NOW()-INTERVAL '1 second' WHERE id=$1`, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.RetryEvents(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, _ := repo.FindBillingEventByProviderID(ctx, "asaas", "evt-provider-failure")
	if recovered.ProcessingError != nil || recovered.ProcessedAt == nil {
		t.Fatal("retry did not recover")
	}
	// A conflicting payload reference cannot grant access for another product.
	alien := renewed
	alien.ID = "pay-alien"
	alien.PaymentLink = "foreign-link"
	alien.Subscription = "sub-alien"
	provider.set(alien)
	if rec := call(payload("evt-alien", "PAYMENT_CONFIRMED", alien)); rec.Code != 200 {
		t.Fatal("unrelated event blocked webhook", rec.Code)
	}
	assertPlan("pro")
	// Slow, older snapshots cannot overwrite the result of a newer authoritative GET.
	snap := &billing.ProviderSnapshot{AccountID: uid, PlanCode: "free", Status: "refunded", SubscriptionID: "sub-yearly", CheckedAt: time.Now().Add(time.Minute)}
	if _, err := repo.ApplyAsaasSnapshot(ctx, snap, ""); err != nil {
		t.Fatal(err)
	}
	snap.PlanCode = "pro"
	snap.Status = "active"
	snap.End = annual.CurrentPeriodEnd
	snap.CheckedAt = time.Now()
	status, err := repo.ApplyAsaasSnapshot(ctx, snap, "")
	if err != nil || status != "ignored_stale_snapshot" {
		t.Fatal(status, err)
	}
	assertPlan("free")

	// Subscription update failures must never acknowledge the event.
	e := &billing.BillingEvent{Provider: "asaas", ProviderEventID: "evt-rollback", EventType: "PAYMENT_CONFIRMED", Payload: []byte(`{}`), CreatedAt: time.Now()}
	if err := repo.CreateBillingEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	invalid := *snap
	invalid.PlanCode = "unknown-plan"
	invalid.CheckedAt = time.Now().Add(2 * time.Minute)
	if _, err := repo.ApplyAsaasSnapshot(ctx, &invalid, e.ID); err == nil {
		t.Fatal("invalid snapshot unexpectedly committed")
	}
	pending, err := repo.FindBillingEventByProviderID(ctx, "asaas", "evt-rollback")
	if err != nil || pending.ProcessedAt != nil {
		t.Fatal("failed transaction acknowledged event", pending, err)
	}
	// It is a diagnostic-only malformed event, not a real provider delivery.
	if _, err := db.Exec(`DELETE FROM billing_events WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	// A manual administrator grant has priority over automated reconciliation.
	if err := postgres.NewUserRepository(db).UpdateUserPlanByAdmin(ctx, uid, "pro"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertPlan("pro")
}
