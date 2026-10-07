package postgres

import (
	"context"
	"database/sql"
	"errors"
	"github.com/JanGustavo/Cron/internal/domain/billing"
)

func (r *BillingRepository) RecordAsaasCheckout(ctx context.Context, userID, linkID, cycle string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO billing_checkouts(provider_link_id,user_id,cycle) VALUES($1,$2,$3)`, linkID, userID, cycle); err != nil {
		return err
	}
	// A purchase intent never replaces paid access or an administrator's grant.
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(user_id,plan_code,status,billing_provider,provider_subscription_id) VALUES($1,'pro','pending','asaas',$2) ON CONFLICT(user_id) DO NOTHING`, userID, linkID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *BillingRepository) GetAsaasAccount(ctx context.Context, userID string) (*billing.AsaasAccount, error) {
	a := &billing.AsaasAccount{UserID: userID, Links: map[string]string{}}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(provider_subscription_id,''),COALESCE(provider_cycle,'') FROM subscriptions WHERE user_id=$1 AND billing_provider='asaas'`, userID).Scan(&a.SubscriptionID, &a.Cycle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT provider_link_id,COALESCE(cycle,'') FROM billing_checkouts WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, cycle string
		if err := rows.Scan(&id, &cycle); err != nil {
			return nil, err
		}
		a.Links[id] = cycle
	}
	return a, rows.Err()
}
func (r *BillingRepository) FindAsaasAccount(ctx context.Context, subscriptionID, linkID string) (*billing.AsaasAccount, error) {
	var uid string
	err := r.db.QueryRowContext(ctx, `SELECT s.user_id FROM subscriptions s WHERE s.billing_provider='asaas' AND (($1<>'' AND s.provider_subscription_id=$1) OR ($2<>'' AND (s.provider_subscription_id=$2 OR EXISTS(SELECT 1 FROM billing_checkouts c WHERE c.user_id=s.user_id AND c.provider_link_id=$2)))) LIMIT 1`, subscriptionID, linkID).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetAsaasAccount(ctx, uid)
}
func (r *BillingRepository) ListAsaasAccounts(ctx context.Context) ([]*billing.AsaasAccount, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id FROM subscriptions WHERE billing_provider='asaas' ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var accounts []*billing.AsaasAccount
	for _, id := range ids {
		a, err := r.GetAsaasAccount(ctx, id)
		if err != nil {
			return nil, err
		}
		if a != nil {
			accounts = append(accounts, a)
		}
	}
	return accounts, nil
}

// Subscription, account grace and event acknowledgement commit together.
func (r *BillingRepository) ApplyAsaasSnapshot(ctx context.Context, s *billing.ProviderSnapshot, eventID string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, s.AccountID); err != nil {
		return "", err
	}
	if eventID != "" {
		var done bool
		if err = tx.QueryRowContext(ctx, `SELECT processed_at IS NOT NULL AND processing_error IS NULL FROM billing_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&done); err != nil {
			return "", err
		}
		if done {
			return "ignored_duplicate", tx.Commit()
		}
	}
	result := "processed"
	var provider string
	var checked sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT billing_provider,provider_checked_at FROM subscriptions WHERE user_id=$1`, s.AccountID).Scan(&provider, &checked); err != nil {
		return "", err
	}
	if provider != "asaas" {
		result = "ignored_manual"
	} else if checked.Valid && !s.CheckedAt.After(checked.Time) {
		result = "ignored_stale_snapshot"
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE subscriptions SET plan_code=$2,status=$3,provider_subscription_id=NULLIF($4,''),provider_customer_id=NULLIF($5,''),provider_cycle=NULLIF($6,''),current_period_start=$7,current_period_end=$8,cancel_at_period_end=$9,provider_checked_at=$10,updated_at=NOW() WHERE user_id=$1`, s.AccountID, s.PlanCode, s.Status, s.SubscriptionID, s.CustomerID, s.Cycle, s.Start, s.End, s.CancelAtPeriodEnd, s.CheckedAt)
		if err != nil {
			return "", err
		}
	}
	if eventID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE billing_events SET user_id=$2,processed_at=NOW(),processing_error=NULL,next_retry_at=NULL WHERE id=$1`, eventID, s.AccountID); err != nil {
			return "", err
		}
	}
	return result, tx.Commit()
}
func (r *BillingRepository) PendingAsaasEvents(ctx context.Context) ([]*billing.BillingEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,payload FROM billing_events WHERE provider='asaas' AND (processed_at IS NULL OR processing_error IS NOT NULL) AND (next_retry_at IS NULL OR next_retry_at<=NOW()) ORDER BY created_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*billing.BillingEvent
	for rows.Next() {
		e := &billing.BillingEvent{}
		if err := rows.Scan(&e.ID, &e.Payload); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
