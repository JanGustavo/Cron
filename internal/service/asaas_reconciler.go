package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JanGustavo/Cron/internal/domain/billing"
	"log"
	"strings"
	"time"
)

type AsaasRepository interface {
	BillingRepository
	RecordAsaasCheckout(context.Context, string, string, string) error
	FindAsaasAccount(context.Context, string, string) (*billing.AsaasAccount, error)
	GetAsaasAccount(context.Context, string) (*billing.AsaasAccount, error)
	ListAsaasAccounts(context.Context) ([]*billing.AsaasAccount, error)
	ApplyAsaasSnapshot(context.Context, *billing.ProviderSnapshot, string) (string, error)
	PendingAsaasEvents(context.Context) ([]*billing.BillingEvent, error)
}
type AsaasReconciler struct {
	Repo     AsaasRepository
	Provider AsaasProvider
	Now      func() time.Time
}

func NewAsaasReconciler(repo AsaasRepository, p AsaasProvider) *AsaasReconciler {
	return &AsaasReconciler{Repo: repo, Provider: p, Now: time.Now}
}
func (r *AsaasReconciler) RecordCheckout(ctx context.Context, userID, linkID, cycle string) error {
	return r.Repo.RecordAsaasCheckout(ctx, userID, linkID, cycle)
}
func isAsaasNotFound(err error) bool {
	var e AsaasHTTPError
	return errors.As(err, &e) && e.Status == 404
}

func (r *AsaasReconciler) ProcessEvent(ctx context.Context, raw []byte) (string, error) {
	if r.Repo == nil {
		return "", fmt.Errorf("Asaas storage unavailable")
	}
	var event billing.AsaasEvent
	if err := json.Unmarshal(raw, &event); err != nil || event.Event == "" {
		return "", fmt.Errorf("invalid Asaas event")
	}
	id := event.ID
	if id == "" {
		id = fmt.Sprintf("legacy:%x", sha256.Sum256(raw))
	}
	if len(id) > 255 {
		return "", fmt.Errorf("invalid Asaas event ID")
	}
	engine := NewEntitlementEngine(r.Repo)
	recorded, duplicate, err := engine.RegisterBillingEvent(ctx, "asaas", id, event.Event, nil, raw)
	if err != nil {
		return "", err
	}
	if duplicate {
		return "ignored_duplicate", nil
	}
	status, err := r.processRecorded(ctx, &event, recorded.ID)
	if err != nil {
		message := err.Error()
		if persistErr := r.Repo.MarkBillingEventProcessed(ctx, recorded.ID, &message); persistErr != nil {
			log.Printf("Asaas retry persistence: %v", persistErr)
		}
	}
	if err != nil {
		return "pending_retry", err
	}
	return status, err
}
func (r *AsaasReconciler) processRecorded(ctx context.Context, event *billing.AsaasEvent, eventID string) (string, error) {
	checkedAt := r.Now()
	var account *billing.AsaasAccount
	var seed *billing.AsaasPayment
	var err error
	switch event.Event {
	case "PAYMENT_CREATED", "PAYMENT_UPDATED", "PAYMENT_CONFIRMED", "PAYMENT_RECEIVED", "PAYMENT_OVERDUE", "PAYMENT_REFUNDED", "PAYMENT_PARTIALLY_REFUNDED", "PAYMENT_CHARGEBACK_REQUESTED", "PAYMENT_CHARGEBACK_DISPUTE", "PAYMENT_AWAITING_CHARGEBACK_REVERSAL", "PAYMENT_RESTORED", "PAYMENT_DELETED":
		if event.Payment == nil || event.Payment.ID == "" {
			return "", fmt.Errorf("Asaas payment missing")
		}
		// Never infer entitlement from the delivery payload; obtain current provider state.
		seed, err = r.Provider.Payment(ctx, event.Payment.ID)
		if err != nil {
			return "", err
		}
		account, err = r.Repo.FindAsaasAccount(ctx, seed.Subscription, seed.PaymentLink)
		if err == nil && account == nil && seed.Subscription != "" {
			sub, e := r.Provider.Subscription(ctx, seed.Subscription)
			if isAsaasNotFound(e) {
				return "ignored_unrelated", r.Repo.MarkBillingEventProcessed(ctx, eventID, nil)
			}
			if e != nil {
				return "", e
			}
			account, err = r.Repo.FindAsaasAccount(ctx, sub.ID, sub.PaymentLink)
			if account != nil && seed.PaymentLink == "" {
				seed.PaymentLink = sub.PaymentLink
			}
		}
	case "SUBSCRIPTION_CREATED", "SUBSCRIPTION_UPDATED", "SUBSCRIPTION_INACTIVATED", "SUBSCRIPTION_DELETED":
		if event.Subscription == nil || event.Subscription.ID == "" {
			return "", fmt.Errorf("Asaas subscription missing")
		}
		sub, e := r.Provider.Subscription(ctx, event.Subscription.ID)
		if isAsaasNotFound(e) {
			account, err = r.Repo.FindAsaasAccount(ctx, event.Subscription.ID, "")
		} else if e != nil {
			return "", e
		} else {
			account, err = r.Repo.FindAsaasAccount(ctx, sub.ID, sub.PaymentLink)
			if account != nil {
				account.SubscriptionID = sub.ID
			}
		}
	default:
		return "ignored_event", r.Repo.MarkBillingEventProcessed(ctx, eventID, nil)
	}
	if err != nil {
		return "", err
	}
	if account == nil {
		return "ignored_unrelated", r.Repo.MarkBillingEventProcessed(ctx, eventID, nil)
	}
	snapshot, err := r.Snapshot(ctx, account, seed, checkedAt)
	if err != nil {
		return "", err
	}
	return r.Repo.ApplyAsaasSnapshot(ctx, snapshot, eventID)
}

func (r *AsaasReconciler) Snapshot(ctx context.Context, a *billing.AsaasAccount, seed *billing.AsaasPayment, checkedAt time.Time) (*billing.ProviderSnapshot, error) {
	remoteSubs := map[string]*billing.AsaasSubscription{}
	knownSubs := map[string]bool{}
	loadSub := func(id string) error {
		if _, ok := remoteSubs[id]; ok {
			return nil
		}
		sub, err := r.Provider.Subscription(ctx, id)
		if isAsaasNotFound(err) {
			remoteSubs[id] = nil
			return nil
		}
		if err != nil {
			return err
		}
		remoteSubs[id] = sub
		return nil
	}
	if strings.HasPrefix(a.SubscriptionID, "sub_") {
		knownSubs[a.SubscriptionID] = true
		if err := loadSub(a.SubscriptionID); err != nil {
			return nil, err
		}
	}
	if seed != nil && seed.Subscription != "" {
		if _, owned := a.Links[seed.PaymentLink]; owned || seed.Subscription == a.SubscriptionID {
			knownSubs[seed.Subscription] = true
			if err := loadSub(seed.Subscription); err != nil {
				return nil, err
			}
		}
	}
	payments, err := r.Provider.Payments(ctx, a.UserID, "")
	if err != nil {
		return nil, err
	}
	for id := range knownSubs {
		if remoteSubs[id] == nil && seed != nil && seed.Subscription != id {
			continue
		}
		more, err := r.Provider.Payments(ctx, a.UserID, id)
		if err != nil {
			return nil, err
		}
		payments = append(payments, more...)
	}
	if seed != nil {
		payments = append(payments, *seed)
	}
	// Linked payments can discover subscriptions when a creation webhook was missed.
	for _, p := range payments {
		if _, owned := a.Links[p.PaymentLink]; owned && p.Subscription != "" {
			knownSubs[p.Subscription] = true
			if err := loadSub(p.Subscription); err != nil {
				return nil, err
			}
		}
	}
	snapshot := &billing.ProviderSnapshot{AccountID: a.UserID, SubscriptionID: a.SubscriptionID, Cycle: a.Cycle, Status: "pending", PlanCode: "free", CheckedAt: checkedAt}
	if sub := remoteSubs[a.SubscriptionID]; sub != nil && (sub.Cycle == "MONTHLY" || sub.Cycle == "YEARLY") {
		snapshot.Cycle = sub.Cycle
		snapshot.CancelAtPeriodEnd = sub.Deleted || sub.Status != "ACTIVE"
		if snapshot.CancelAtPeriodEnd {
			snapshot.Status = "canceled"
		}
	}
	bestPaymentID := ""
	seen := map[string]bool{}
	ownedCount := 0
	for _, p := range payments {
		if p.ID == "" || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		_, linked := a.Links[p.PaymentLink]
		if !linked && !knownSubs[p.Subscription] {
			continue
		}
		ownedCount++
		switch p.Status {
		case "PENDING", "OVERDUE", "CONFIRMED", "RECEIVED", "REFUNDED", "REFUND_REQUESTED", "REFUND_IN_PROGRESS", "CHARGEBACK_REQUESTED", "CHARGEBACK_DISPUTE", "AWAITING_CHARGEBACK_REVERSAL", "DUNNING_REQUESTED", "DUNNING_RECEIVED", "AUTHORIZED", "AWAITING_RISK_ANALYSIS":
		default:
			if !p.Deleted {
				return nil, fmt.Errorf("Asaas payment status unsupported")
			}
		}
		if p.Status == "OVERDUE" {
			snapshot.Status = "past_due"
		}
		if p.Status == "REFUNDED" || strings.HasPrefix(p.Status, "CHARGEBACK") {
			snapshot.Status = "refunded"
		}
		if p.Deleted || (p.Status != "CONFIRMED" && p.Status != "RECEIVED" && p.Status != "REFUND_REQUESTED" && p.Status != "REFUND_IN_PROGRESS") {
			continue
		}
		cycle := a.Links[p.PaymentLink]
		cancelled := false
		if p.Subscription != "" {
			if err := loadSub(p.Subscription); err != nil {
				return nil, err
			}
			sub := remoteSubs[p.Subscription]
			if sub != nil {
				cycle = sub.Cycle
				cancelled = sub.Deleted || sub.Status != "ACTIVE"
			} else {
				cancelled = true
				if cycle == "" {
					cycle = a.Cycle
				}
			}
		}
		if cycle == "" && linked {
			link, err := r.Provider.Link(ctx, p.PaymentLink)
			if err != nil {
				return nil, err
			}
			cycle = link.Cycle
			a.Links[p.PaymentLink] = cycle
		}
		start, end, err := AsaasPaidPeriod(p, cycle)
		if err != nil {
			return nil, err
		}
		if snapshot.End == nil || end.After(*snapshot.End) || end.Equal(*snapshot.End) && p.ID > bestPaymentID {
			bestPaymentID = p.ID
			snapshot.Start = &start
			snapshot.End = &end
			snapshot.Cycle = cycle
			snapshot.CustomerID = p.Customer
			snapshot.CancelAtPeriodEnd = cancelled
			if p.Subscription != "" {
				snapshot.SubscriptionID = p.Subscription
			} else {
				snapshot.SubscriptionID = p.PaymentLink
			}
		}
	}
	if snapshot.End != nil {
		if snapshot.End.After(checkedAt) {
			snapshot.Status = "active"
			snapshot.PlanCode = "pro"
		} else {
			snapshot.Status = "expired"
		}
	} else if ownedCount == 0 && len(a.Links) == 0 {
		return nil, fmt.Errorf("Asaas reference has no verifiable payments or checkout")
	}
	return snapshot, nil
}

// Provider dates are stable across CONFIRMED/RECEIVED; settlement delivery time is irrelevant.
func AsaasPaidPeriod(p billing.AsaasPayment, cycle string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	date := p.ClientPaymentDate
	if date == "" {
		date = p.ConfirmedDate
	}
	if date == "" {
		date = p.PaymentDate
	}
	paid, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("Asaas paid date missing or invalid")
	}
	anchor := paid
	if p.DueDate != "" {
		due, err := time.ParseInLocation("2006-01-02", p.DueDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("Asaas due date invalid")
		}
		if due.After(anchor) {
			anchor = due
		}
	}
	months := 1
	if cycle == "YEARLY" {
		months = 12
	} else if cycle != "MONTHLY" {
		return time.Time{}, time.Time{}, fmt.Errorf("Asaas billing cycle missing or unsupported")
	}
	// Calendar clamping avoids Jan 31 becoming March 3 after adding one month.
	month := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, 0, 0, 0, 0, loc)
	day := anchor.Day()
	last := month.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	end := time.Date(month.Year(), month.Month(), day, 0, 0, 0, 0, loc)
	return anchor, end, nil
}
func (r *AsaasReconciler) Reconcile(ctx context.Context) error {
	accounts, err := r.Repo.ListAsaasAccounts(ctx)
	if err != nil {
		return err
	}
	failures := 0
	for _, a := range accounts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		snap, err := r.Snapshot(ctx, a, nil, r.Now())
		if err == nil {
			_, err = r.Repo.ApplyAsaasSnapshot(ctx, snap, "")
		}
		if err != nil {
			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("Asaas reconciliation: %d references/events require retry", failures)
	}
	return nil
}

func (r *AsaasReconciler) RetryEvents(ctx context.Context) error {
	failures := 0
	events, err := r.Repo.PendingAsaasEvents(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := r.ProcessEvent(ctx, e.Payload); err != nil {
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("Asaas retry: %d events pending", failures)
	}
	return nil
}
func (r *AsaasReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	nextSync := time.Time{}
	for {
		attempt, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := r.RetryEvents(attempt); err != nil {
			log.Printf("%v", err)
		}
		if !r.Now().Before(nextSync) {
			if err := r.Reconcile(attempt); err != nil {
				log.Printf("%v", err)
			}
			nextSync = r.Now().Add(15 * time.Minute)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
