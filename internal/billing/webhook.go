package billing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"shellhaki/envi/internal/bachs"
)

// Applying Bachs webhooks.

// ErrUnmatched marks an event that names no organization Envi knows.
var ErrUnmatched = errors.New("event matches no organization")

// Apply applies one verified event.
func (s Service) Apply(ctx context.Context, event bachs.Event) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `INSERT INTO bachs_events (id, type) VALUES ($1, $2) ON CONFLICT DO NOTHING`, event.ID, event.Type)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already applied
	}

	switch event.Type {
	case "checkout.completed":
		err = s.checkoutCompleted(ctx, tx, event)
	case "payment_method.saved":
		err = s.cardSaved(ctx, tx, event)
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		err = s.subscriptionChanged(ctx, tx, event)
	case "collection.succeeded", "collection.failed":
		err = s.collection(ctx, tx, event)
	default:
		// Recorded and ignored: every event Bachs sends arrives here, and most are none of billing's business.
	}
	if errors.Is(err, ErrUnmatched) {
		log.Printf("bachs event %s (%s): %v", event.ID, event.Type, err)
		err = nil
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// payload is the union of fields billing reads from any event's data.
type payload struct {
	Mode              string            `json:"mode"`
	Status            string            `json:"status"`
	Reference         string            `json:"reference"`
	ChargeID          string            `json:"charge_id"`
	SubscriptionID    string            `json:"subscription_id"`
	ProductID         string            `json:"product_id"`
	CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
	CurrentPeriodEnd  *time.Time        `json:"current_period_end"`
	CardBrand         string            `json:"card_brand"`
	CardLast4         string            `json:"card_last4"`
	Reason            string            `json:"reason"`
	Metadata          map[string]string `json:"metadata"`
	// Bachs sends the customer as an id, or an object with "id" or "customer_id".
	Customer   json.RawMessage `json:"customer"`
	CustomerID string          `json:"customer_id"`
}

func decode(event bachs.Event) payload {
	var p payload
	_ = json.Unmarshal(event.Data, &p) // lenient: a field that does not fit stays empty
	return p
}

// customerID reads the customer however this event spells it.
func (p payload) customerID() string {
	if p.CustomerID != "" {
		return p.CustomerID
	}
	var bare string
	if json.Unmarshal(p.Customer, &bare) == nil && bare != "" {
		return bare
	}
	var obj struct {
		ID         string `json:"id"`
		CustomerID string `json:"customer_id"`
	}
	if json.Unmarshal(p.Customer, &obj) == nil {
		if obj.CustomerID != "" {
			return obj.CustomerID
		}
		return obj.ID
	}
	return ""
}

// orgFor finds which organization an event is about.
func orgFor(ctx context.Context, tx pgx.Tx, p payload) (string, error) {
	if id := p.Metadata["org_id"]; id != "" {
		var found string
		err := tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id::text = $1`, id).Scan(&found)
		if err == nil {
			return found, nil
		}
	}
	if customer := p.customerID(); customer != "" {
		var found string
		err := tx.QueryRow(ctx, `SELECT org_id FROM billing_accounts WHERE bachs_customer_id = $1`, customer).Scan(&found)
		if err == nil {
			return found, nil
		}
	}
	return "", ErrUnmatched
}

func ensureRowTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO billing_accounts (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, orgID)
	return err
}

// checkoutCompleted is the only signal for a save-card checkout.
func (s Service) checkoutCompleted(ctx context.Context, tx pgx.Tx, event bachs.Event) error {
	p := decode(event)
	if p.Mode != "setup" {
		return nil // subscription and payment checkouts report through their own events
	}
	orgID, err := orgFor(ctx, tx, p)
	if err != nil {
		return err
	}
	if err := ensureRowTx(ctx, tx, orgID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_accounts SET card_saved = true, updated_at = now() WHERE org_id = $1`, orgID); err != nil {
		return err
	}
	// The card was saved in order to start pay as you go, in the currency chosen when the checkout was made.
	if p.Metadata["plan"] == PAYG {
		currency := strings.ToUpper(p.Metadata["currency"])
		if _, ok := PerMember[currency]; !ok {
			currency = "USD"
		}
		if _, err := tx.Exec(ctx, `UPDATE billing_accounts SET plan = 'payg', currency = $2 WHERE org_id = $1`, orgID, currency); err != nil {
			return err
		}
		// Everybody already there counts from today.
		if _, err := tx.Exec(ctx, `
			INSERT INTO billable_members (org_id, user_id, month)
			SELECT $1, c.subject_user_id, $2::date FROM (`+collaboratorsSQL+`) c
			ON CONFLICT DO NOTHING
		`, orgID, monthOf(s.now())); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) cardSaved(ctx context.Context, tx pgx.Tx, event bachs.Event) error {
	p := decode(event)
	orgID, err := orgFor(ctx, tx, p)
	if err != nil {
		return err
	}
	if err := ensureRowTx(ctx, tx, orgID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE billing_accounts SET card_saved = true, card_brand = $2, card_last4 = $3, updated_at = now()
		 WHERE org_id = $1
	`, orgID, p.CardBrand, p.CardLast4)
	return err
}

func (s Service) subscriptionChanged(ctx context.Context, tx pgx.Tx, event bachs.Event) error {
	p := decode(event)
	// Only the Team product moves an organization onto Team.
	if s.TeamProductID != "" && p.ProductID != "" && p.ProductID != s.TeamProductID {
		return nil
	}
	orgID, err := orgFor(ctx, tx, p)
	if err != nil {
		return err
	}
	if err := ensureRowTx(ctx, tx, orgID); err != nil {
		return err
	}

	var last *time.Time
	if err := tx.QueryRow(ctx, `SELECT last_event_at FROM billing_accounts WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&last); err != nil {
		return err
	}
	if last != nil && !event.CreatedAt.IsZero() && event.CreatedAt.Before(*last) {
		return nil // an older state arriving late
	}

	status := p.Status
	if event.Type == "customer.subscription.deleted" {
		status = "canceled"
	}

	// Paid for, or being retried.
	plan := Team
	switch status {
	case "canceled", "unpaid", "incomplete_expired":
		plan = Free
	}
	_, err = tx.Exec(ctx, `
		UPDATE billing_accounts
		   SET plan = CASE WHEN $3 = 'free' AND plan <> 'team' THEN plan ELSE $3 END,
		       subscription_id = COALESCE(NULLIF($2, ''), subscription_id),
		       subscription_status = $4,
		       cancel_at_period_end = $5,
		       current_period_end = $6,
		       last_event_at = $7,
		       updated_at = now()
		 WHERE org_id = $1
	`, orgID, p.SubscriptionID, plan, status, p.CancelAtPeriodEnd, p.CurrentPeriodEnd, event.CreatedAt)
	return err
}

// collection settles a pay-as-you-go month.
func (s Service) collection(ctx context.Context, tx pgx.Tx, event bachs.Event) error {
	p := decode(event)
	if !strings.HasPrefix(p.Reference, "payg:") {
		return nil
	}
	parts := strings.Split(p.Reference, ":")
	if len(parts) != 4 {
		return ErrUnmatched
	}
	orgID, month := parts[1], parts[2]+"-01"

	var chargeID string
	var attempts int
	err := tx.QueryRow(ctx, `
		SELECT id, attempts FROM payg_charges WHERE org_id::text = $1 AND month = $2::date FOR UPDATE
	`, orgID, month).Scan(&chargeID, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnmatched
	}
	if err != nil {
		return err
	}

	// Ignore a late report about an earlier attempt; the attempt number ends the reference.
	if parts[3] != strconv.Itoa(attempts) {
		return nil
	}

	if event.Type == "collection.succeeded" {
		_, err = tx.Exec(ctx, `
			UPDATE payg_charges SET status = 'succeeded', bachs_charge_id = COALESCE(NULLIF($2, ''), bachs_charge_id),
			       next_attempt_at = NULL, last_error = '', updated_at = now()
			 WHERE id = $1
		`, chargeID, p.ChargeID)
		return err
	}

	var next *time.Time
	if attempts < MaxAttempts && attempts-1 < len(retryAfter) {
		at := s.now().Add(retryAfter[attempts-1])
		next = &at
	}
	reason := p.Reason
	if reason == "" {
		reason = "card refused"
	}
	_, err = tx.Exec(ctx, `
		UPDATE payg_charges SET status = 'failed', next_attempt_at = $2, last_error = $3, updated_at = now()
		 WHERE id = $1
	`, chargeID, next, reason)
	return err
}
