package billing

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

/*
The job that turns pay-as-you-go months into charges.

It runs inside the API process every hour. Each pass is safe to repeat and
safe to run on two servers at once, because of three things:

  - A month gets one payg_charges row, enforced by UNIQUE (org_id, month), so
    two passes cannot both create it.
  - Every charge goes to Bachs with an idempotency key naming the
    organization, month and attempt, so sending the same attempt twice returns
    the first charge instead of making a second.
  - A pass holds a Postgres advisory lock, so on several servers only one does
    the work at a time. The first two make that an efficiency, not a
    correctness requirement.
*/

// jobLock is the advisory lock key for the charge job. Any constant works as
// long as nothing else in the database uses it.
const jobLock int64 = 0x656e7669_62696c6c // "envibill"

// Run works through billing every interval until ctx ends.
func (s Service) Run(ctx context.Context, interval time.Duration) {
	if !s.Enabled() {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.Pass(ctx); err != nil {
			log.Printf("billing pass: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Pass does one round: count, close finished months, retry what is due.
func (s Service) Pass(ctx context.Context) error {
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, jobLock).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil // another server is mid-pass
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, jobLock)

	if err := s.sweep(ctx); err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	if err := s.closeMonths(ctx); err != nil {
		return fmt.Errorf("close months: %w", err)
	}
	if err := s.retryDue(ctx); err != nil {
		return fmt.Errorf("retries: %w", err)
	}
	return nil
}

// sweep records everybody who currently counts, for every pay-as-you-go
// organization, against this month. Somebody removed before the next sweep
// has already been recorded, which is the point: they had access this month.
func (s Service) sweep(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO billable_members (org_id, user_id, month)
		SELECT b.org_id, g.subject_user_id, $1::date
		  FROM billing_accounts b
		  JOIN projects p ON p.org_id = b.org_id
		  JOIN access_grants g ON g.project_id = p.id
		 WHERE b.plan = 'payg'
		   AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = b.org_id AND m.user_id = g.subject_user_id)
		ON CONFLICT DO NOTHING
	`, monthOf(s.now()))
	return err
}

func (s Service) sweepOrg(ctx context.Context, orgID string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO billable_members (org_id, user_id, month)
		SELECT $1, c.subject_user_id, $2::date FROM (`+collaboratorsSQL+`) c
		ON CONFLICT DO NOTHING
	`, orgID, monthOf(s.now()))
	return err
}

/*
closeMonths creates the charge for every finished month that has none yet.

A month is finished once the calendar has moved past it. Its total is the
people counted in it times the per-person price, plus any earlier months that
were too small to charge on their own. If that total is still below the
minimum, the month is carried too; otherwise it is charged.
*/
func (s Service) closeMonths(ctx context.Context) error {
	current := monthOf(s.now())
	rows, err := s.DB.Query(ctx, `
		SELECT bm.org_id, bm.month, count(*)
		  FROM billable_members bm
		 WHERE bm.month < $1
		   AND NOT EXISTS (SELECT 1 FROM payg_charges pc WHERE pc.org_id = bm.org_id AND pc.month = bm.month)
		 GROUP BY bm.org_id, bm.month
		 ORDER BY bm.month
	`, current)
	if err != nil {
		return err
	}
	type open struct {
		org     string
		month   time.Time
		members int
	}
	var months []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.org, &o.month, &o.members); err != nil {
			rows.Close()
			return err
		}
		months = append(months, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range months {
		if err := s.closeMonth(ctx, m.org, m.month, m.members); err != nil {
			log.Printf("billing: closing %s for %s: %v", m.month.Format("2006-01"), m.org, err)
		}
	}
	return nil
}

func (s Service) closeMonth(ctx context.Context, orgID string, month time.Time, members int) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	a, err := account(ctx, tx, orgID)
	if err != nil {
		return err
	}
	currency := a.Currency
	amount := int64(members) * PerMember[currency]

	// Earlier months in the same currency that were too small to charge. They
	// are taken over by this month as it is created, so no later month can
	// count them a second time.
	var carried int64
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(amount_minor + carried_minor), 0) FROM payg_charges
		 WHERE org_id = $1 AND status = 'carried' AND currency = $2
	`, orgID, currency).Scan(&carried); err != nil {
		return err
	}

	status := "processing"
	if amount+carried < Minimum[currency] {
		status = "carried"
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO payg_charges (org_id, month, currency, members, amount_minor, carried_minor, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (org_id, month) DO NOTHING
		RETURNING id
	`, orgID, month, currency, members, amount, carried, status).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil // another pass got here first
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE payg_charges SET status = 'settled', settled_by = $3, updated_at = now()
		 WHERE org_id = $1 AND status = 'carried' AND currency = $2 AND id <> $3
	`, orgID, currency, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if status == "carried" {
		return nil
	}
	return s.attempt(ctx, id)
}

// retryDue charges again every failed month whose wait is over.
func (s Service) retryDue(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `
		SELECT id FROM payg_charges
		 WHERE status = 'failed' AND attempts < $1 AND next_attempt_at <= $2
	`, MaxAttempts, s.now())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.attempt(ctx, id); err != nil {
			log.Printf("billing: retrying %s: %v", id, err)
		}
	}
	return rows.Err()
}

/*
attempt sends one charge for a month.

The attempt number is part of the idempotency key. Within one attempt a retry
of the request returns the original charge; a new attempt needs a new key, or
Bachs would hand back the original failure instead of trying the card again.

The row is marked processing before Bachs is called and only the webhook marks
it paid. A charge whose reply was lost in transit therefore stays processing
until Bachs reports on it, rather than being charged again.
*/
func (s Service) attempt(ctx context.Context, chargeID string) error {
	var orgID, currency string
	var month time.Time
	var amount, carried int64
	var attempts int
	err := s.DB.QueryRow(ctx, `
		UPDATE payg_charges SET attempts = attempts + 1, status = 'processing', updated_at = now()
		 WHERE id = $1 AND status IN ('processing', 'failed')
		RETURNING org_id, month, currency, amount_minor, carried_minor, attempts
	`, chargeID).Scan(&orgID, &month, &currency, &amount, &carried, &attempts)
	if err != nil {
		return err
	}

	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return err
	}
	if a.CustomerID == "" || !a.CardSaved {
		return s.fail(ctx, chargeID, attempts, "no saved card for this organization")
	}

	key := fmt.Sprintf("payg:%s:%s:%d", orgID, month.Format("2006-01"), attempts)
	description := fmt.Sprintf("Envi pay as you go, %s", month.Format("January 2006"))
	charge, err := s.Bachs.ChargeSavedCard(ctx, a.CustomerID, amount+carried, currency, description, key)
	if err != nil {
		return s.fail(ctx, chargeID, attempts, err.Error())
	}
	if charge.Status == "failed" || charge.Status == "FAILED" {
		return s.fail(ctx, chargeID, attempts, "card refused")
	}
	_, err = s.DB.Exec(ctx, `UPDATE payg_charges SET bachs_charge_id = $2, updated_at = now() WHERE id = $1`, chargeID, charge.ID)
	return err
}

// fail records a refused attempt and schedules the next, if there is one.
func (s Service) fail(ctx context.Context, chargeID string, attempts int, reason string) error {
	var next *time.Time
	if attempts < MaxAttempts && attempts-1 < len(retryAfter) {
		at := s.now().Add(retryAfter[attempts-1])
		next = &at
	}
	_, err := s.DB.Exec(ctx, `
		UPDATE payg_charges SET status = 'failed', next_attempt_at = $2, last_error = $3, updated_at = now()
		 WHERE id = $1
	`, chargeID, next, reason)
	return err
}
