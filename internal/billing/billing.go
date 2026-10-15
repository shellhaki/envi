// Package billing decides what an organization owes and whether it may add
// another person. Bachs moves the money; everything about what is owed and
// when is decided here, against Envi's own database.
//
// The plans are in BILLING.md. In short: free is the owner alone, pay as you
// go charges per extra person per month to a saved card in USD or NGN, and
// team is a flat USD subscription.
//
// "Extra person" means somebody holding an access grant on one of the
// organization's projects who is not a member of the organization. That is
// what accepting an invitation creates; counting memberships instead would
// find nobody and bill nothing.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/bachs"
)

// Plans.
const (
	Free = "free"
	PAYG = "payg"
	Team = "team"
)

// PerMember is what pay as you go charges per extra person per month, in
// minor units. Changing a price is changing a number here.
var PerMember = map[string]int64{
	"USD": 40,    // $0.40
	"NGN": 60000, // ₦600
}

/*
Minimum is the smallest monthly total worth charging, in minor units. Anything
below it is carried into the next month instead.

USD cards cost 5% plus a fixed $0.40 a charge, so a $0.40 month would cost
more to collect than it brings in. $3 keeps about 82% after fees. NGN local
cards cost a flat 2% with no fixed part, so every naira month is worth
charging.
*/
var Minimum = map[string]int64{
	"USD": 300,
	"NGN": 0,
}

// TeamPriceMinor is shown on the billing page. The amount actually charged is
// whatever the Bachs product says; keep the two in step.
const TeamPriceMinor int64 = 400

// MaxAttempts is how many times a month is charged before the organization is
// past due: the first attempt, then after 3 days, then after 7.
const MaxAttempts = 3

var retryAfter = []time.Duration{3 * 24 * time.Hour, 7 * 24 * time.Hour}

// Provider is the part of Bachs billing needs. An interface so the service can
// be tested without a network, and so a self-hosted instance has none at all.
type Provider interface {
	CreateCustomer(ctx context.Context, email, name string, metadata map[string]string) (string, error)
	SaveCardCheckout(ctx context.Context, customerID, successURL, cancelURL string, metadata map[string]string) (bachs.Checkout, error)
	SubscriptionCheckout(ctx context.Context, customerID, productID, successURL, cancelURL string, metadata map[string]string) (bachs.Checkout, error)
	CancelSubscription(ctx context.Context, subscriptionID string) error
	ChargeSavedCard(ctx context.Context, customerID string, minor int64, currency, description, key string) (bachs.Charge, error)
	PortalURL(ctx context.Context, customerID string) (string, error)
}

// Service is billing for one instance.
type Service struct {
	DB *pgxpool.Pool
	// Bachs is nil on any instance that does not bill — self-hosted, or hosted
	// with no keys configured — and every method is then a no-op that allows.
	Bachs         Provider
	TeamProductID string
	WebURL        string
	// ReturnURL is where Bachs sends somebody after checkout, when that must
	// differ from WebURL. Bachs refuses localhost as a return address even in
	// the sandbox, so local testing points this at any public URL. Empty
	// means WebURL.
	ReturnURL string
	// WebhookSecret verifies Bachs's webhooks.
	WebhookSecret string
	// Now is overridable so tests can stand at a month boundary.
	Now func() time.Time
}

// Enabled reports whether this instance charges anybody.
func (s Service) Enabled() bool { return s.Bachs != nil }

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// monthOf is the first day of t's month, which is how months are stored.
func monthOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// --- refusals ---------------------------------------------------------------

/*
Refusal is why an organization may not add somebody, in a form the API turns
into 402 Payment Required.

Code is what a script acts on, so it is stable and short; Message is for a
person, so it says what to do next.
*/
type Refusal struct {
	Code    string `json:"code"`
	Message string `json:"error"`
	Plan    string `json:"plan"`
}

func (r *Refusal) Error() string { return r.Message }

// IsRefusal reports whether err is a billing refusal, for mapping to 402.
func IsRefusal(err error) (*Refusal, bool) {
	var r *Refusal
	ok := errors.As(err, &r)
	return r, ok
}

func refuse(code, plan, message string) error {
	return &Refusal{Code: code, Plan: plan, Message: message}
}

// --- accounts ---------------------------------------------------------------

// Account is an organization's billing state.
type Account struct {
	OrgID              string     `json:"org_id"`
	Plan               string     `json:"plan"`
	Currency           string     `json:"currency"`
	CustomerID         string     `json:"-"`
	CardSaved          bool       `json:"card_saved"`
	CardBrand          string     `json:"card_brand"`
	CardLast4          string     `json:"card_last4"`
	SubscriptionStatus string     `json:"subscription_status"`
	CancelAtPeriodEnd  bool       `json:"cancel_at_period_end"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end"`
	PAYGCap            int        `json:"payg_cap"`
	subscriptionID     string
}

// teamActive is whether a team subscription is currently paid for. past_due
// still counts: Bachs is retrying the card, and nobody loses access while it
// does.
func (a Account) teamActive() bool {
	if a.Plan != Team {
		return false
	}
	switch a.SubscriptionStatus {
	case "active", "trialing", "past_due":
		return true
	}
	return false
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// account loads an organization's state. No row means free.
func account(ctx context.Context, q querier, orgID string) (Account, error) {
	a := Account{OrgID: orgID, Plan: Free, Currency: "USD", PAYGCap: 20}
	var customer, subscription *string
	err := q.QueryRow(ctx, `
		SELECT plan, currency, bachs_customer_id, card_saved, card_brand, card_last4,
		       subscription_id, subscription_status, cancel_at_period_end, current_period_end, payg_cap
		  FROM billing_accounts WHERE org_id = $1
	`, orgID).Scan(&a.Plan, &a.Currency, &customer, &a.CardSaved, &a.CardBrand, &a.CardLast4,
		&subscription, &a.SubscriptionStatus, &a.CancelAtPeriodEnd, &a.CurrentPeriodEnd, &a.PAYGCap)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil
	}
	if customer != nil {
		a.CustomerID = *customer
	}
	if subscription != nil {
		a.subscriptionID = *subscription
	}
	return a, err
}

// ensureRow makes sure an organization has a billing row to update.
func (s Service) ensureRow(ctx context.Context, orgID string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO billing_accounts (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, orgID)
	return err
}

// --- counting ----------------------------------------------------------------

// collaboratorsSQL selects everybody who counts as an extra person for $1 right
// now: holds a grant on one of its projects and is not one of its members.
const collaboratorsSQL = `
	SELECT DISTINCT g.subject_user_id
	  FROM access_grants g
	  JOIN projects p ON p.id = g.project_id
	 WHERE p.org_id = $1
	   AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = $1 AND m.user_id = g.subject_user_id)`

func (s Service) extraCount(ctx context.Context, q querier, orgID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM (`+collaboratorsSQL+`) c`, orgID).Scan(&n)
	return n, err
}

// alreadyIn reports whether email is already counted for orgID — a member, or
// somebody who already holds a grant. Adding them to another project is not a
// new person and costs nothing.
func (s Service) alreadyIn(ctx context.Context, q querier, orgID, email string) (bool, error) {
	var in bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM users u
		   WHERE lower(u.email) = lower($2)
		     AND (u.id IN (`+collaboratorsSQL+`)
		          OR EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = $1 AND m.user_id = u.id)))
	`, orgID, email).Scan(&in)
	return in, err
}

// pastDue reports whether a pay-as-you-go month has run out of retries.
func (s Service) pastDue(ctx context.Context, q querier, orgID string) (bool, error) {
	var due bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM payg_charges WHERE org_id = $1 AND status = 'failed' AND attempts >= $2)
	`, orgID, MaxAttempts).Scan(&due)
	return due, err
}

/*
AllowNewPerson decides whether orgID may give email access.

It is called when an invitation is sent and again when it is accepted:
checking only at sending lets an organization one place below its cap send ten
invitations and land eleven people.

Nothing already in place is ever taken away by this. It refuses the next
addition, and only that.
*/
func (s Service) AllowNewPerson(ctx context.Context, orgID, email string) error {
	if !s.Enabled() {
		return nil
	}
	in, err := s.alreadyIn(ctx, s.DB, orgID, email)
	if err != nil || in {
		return err
	}
	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return err
	}

	if a.teamActive() {
		return nil
	}
	if a.Plan != PAYG {
		return refuse("payment_required", Free,
			"the free plan is for one person; choose pay as you go or Team on the billing page to add anyone else")
	}
	if !a.CardSaved {
		return refuse("payment_required", PAYG,
			"pay as you go needs a saved card before anyone can be added; finish setting it up on the billing page")
	}
	due, err := s.pastDue(ctx, s.DB, orgID)
	if err != nil {
		return err
	}
	if due {
		return refuse("payg_past_due", PAYG,
			"a pay-as-you-go charge has failed; update the card on the billing page before adding anyone else")
	}
	extras, err := s.extraCount(ctx, s.DB, orgID)
	if err != nil {
		return err
	}
	if extras >= a.PAYGCap {
		return refuse("payg_cap_reached", PAYG,
			fmt.Sprintf("pay as you go is capped at %d extra people; Team has no cap", a.PAYGCap))
	}
	return nil
}

// RecordPerson notes that userID counts for orgID this month. Called when an
// invitation is accepted; the hourly sweep catches everybody else.
func (s Service) RecordPerson(ctx context.Context, orgID, userID string) error {
	if !s.Enabled() {
		return nil
	}
	_, err := s.DB.Exec(ctx, `
		INSERT INTO billable_members (org_id, user_id, month)
		SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM billing_accounts WHERE org_id = $1 AND plan = 'payg')
		ON CONFLICT DO NOTHING
	`, orgID, userID, monthOf(s.now()))
	return err
}

// --- the billing page ---------------------------------------------------------

// Person is one extra person, for the usage list.
type Person struct {
	Email string `json:"email"`
}

// Charge is one month on pay as you go.
type Charge struct {
	Month        string `json:"month"`
	Currency     string `json:"currency"`
	Members      int    `json:"members"`
	AmountMinor  int64  `json:"amount_minor"`
	CarriedMinor int64  `json:"carried_minor"`
	Status       string `json:"status"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"last_error"`
}

// Overview is everything the billing page shows.
type Overview struct {
	Enabled bool    `json:"enabled"`
	Account Account `json:"account"`
	// People counted right now, and how many have counted this month.
	People         []Person `json:"people"`
	CountedMonth   int      `json:"counted_this_month"`
	ProjectedMinor int64    `json:"projected_minor"`
	PastDue        bool     `json:"past_due"`
	Charges        []Charge `json:"charges"`
	Prices         struct {
		PerMember map[string]int64 `json:"per_member"`
		Minimum   map[string]int64 `json:"minimum"`
		Team      int64            `json:"team"`
	} `json:"prices"`
	TeamAvailable bool `json:"team_available"`
}

func (s Service) Overview(ctx context.Context, orgID string) (Overview, error) {
	var o Overview
	o.Enabled = s.Enabled()
	o.Prices.PerMember = PerMember
	o.Prices.Minimum = Minimum
	o.Prices.Team = TeamPriceMinor
	o.TeamAvailable = s.TeamProductID != ""
	o.People = []Person{}
	o.Charges = []Charge{}

	var err error
	if o.Account, err = account(ctx, s.DB, orgID); err != nil {
		return o, err
	}

	rows, err := s.DB.Query(ctx, `SELECT u.email FROM users u WHERE u.id IN (`+collaboratorsSQL+`) ORDER BY u.email`, orgID)
	if err != nil {
		return o, err
	}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.Email); err != nil {
			rows.Close()
			return o, err
		}
		o.People = append(o.People, p)
	}
	rows.Close()

	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM billable_members WHERE org_id = $1 AND month = $2`,
		orgID, monthOf(s.now())).Scan(&o.CountedMonth); err != nil {
		return o, err
	}
	// On pay as you go, this month so far is at least everybody counted plus
	// anybody here now who the sweep has not reached yet.
	if o.Account.Plan == PAYG {
		billed := o.CountedMonth
		if len(o.People) > billed {
			billed = len(o.People)
		}
		o.ProjectedMinor = int64(billed) * PerMember[o.Account.Currency]
	}
	if o.PastDue, err = s.pastDue(ctx, s.DB, orgID); err != nil {
		return o, err
	}

	history, err := s.DB.Query(ctx, `
		SELECT to_char(month, 'YYYY-MM'), currency, members, amount_minor, carried_minor, status, attempts, last_error
		  FROM payg_charges WHERE org_id = $1 ORDER BY month DESC LIMIT 12
	`, orgID)
	if err != nil {
		return o, err
	}
	defer history.Close()
	for history.Next() {
		var c Charge
		if err := history.Scan(&c.Month, &c.Currency, &c.Members, &c.AmountMinor, &c.CarriedMinor,
			&c.Status, &c.Attempts, &c.LastError); err != nil {
			return o, err
		}
		o.Charges = append(o.Charges, c)
	}
	return o, history.Err()
}

// --- changing plan ------------------------------------------------------------

func (s Service) returnBase() string {
	if s.ReturnURL != "" {
		return s.ReturnURL
	}
	return s.WebURL
}

func (s Service) returnURLs() (success, cancel string) {
	base := strings.TrimRight(s.returnBase(), "/")
	return base + "/billing/success", base + "/dashboard/billing?checkout=cancelled"
}

/*
customer returns the organization's Bachs customer, creating it the first time.

The row is locked while the customer is created, so two clicks in a row make
one customer rather than two — a duplicate would split a card from the
subscription it is meant to pay.
*/
func (s Service) customer(ctx context.Context, orgID, email string) (string, error) {
	if err := s.ensureRow(ctx, orgID); err != nil {
		return "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var existing *string
	if err := tx.QueryRow(ctx, `SELECT bachs_customer_id FROM billing_accounts WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&existing); err != nil {
		return "", err
	}
	if existing != nil && *existing != "" {
		return *existing, nil
	}
	id, err := s.Bachs.CreateCustomer(ctx, email, email, map[string]string{"org_id": orgID})
	if err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_accounts SET bachs_customer_id = $2, updated_at = now() WHERE org_id = $1`, orgID, id); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

var errDisabled = errors.New("billing is not enabled on this instance")

/*
StartPAYG moves an organization onto pay as you go in currency.

With a card already saved this is immediate and returns no URL. Otherwise it
returns a Bachs checkout that saves a card without charging it; the plan only
changes when the checkout.completed webhook says the card was saved, so
abandoning the checkout leaves everything as it was.
*/
func (s Service) StartPAYG(ctx context.Context, orgID, email, currency string) (string, error) {
	if !s.Enabled() {
		return "", errDisabled
	}
	if _, ok := PerMember[currency]; !ok {
		return "", errors.New("currency must be USD or NGN")
	}
	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return "", err
	}
	if a.teamActive() && !a.CancelAtPeriodEnd {
		return "", errors.New("cancel Team first; pay as you go starts when it ends")
	}
	if a.CardSaved {
		if err := s.ensureRow(ctx, orgID); err != nil {
			return "", err
		}
		if _, err := s.DB.Exec(ctx, `UPDATE billing_accounts SET plan = 'payg', currency = $2, updated_at = now() WHERE org_id = $1`, orgID, currency); err != nil {
			return "", err
		}
		return "", s.sweepOrg(ctx, orgID)
	}

	customerID, err := s.customer(ctx, orgID, email)
	if err != nil {
		return "", err
	}
	success, cancel := s.returnURLs()
	checkout, err := s.Bachs.SaveCardCheckout(ctx, customerID, success, cancel, map[string]string{
		"org_id": orgID, "plan": PAYG, "currency": currency,
	})
	if err != nil {
		return "", fmt.Errorf("save-card checkout: %w", err)
	}
	return checkout.URL, nil
}

// StartTeam returns a checkout for the Team subscription. The plan changes when
// customer.subscription.created arrives, not here.
func (s Service) StartTeam(ctx context.Context, orgID, email string) (string, error) {
	if !s.Enabled() {
		return "", errDisabled
	}
	if s.TeamProductID == "" {
		return "", errors.New("Team is not set up on this instance yet")
	}
	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return "", err
	}
	if a.teamActive() {
		return "", errors.New("this organization is already on Team")
	}
	customerID, err := s.customer(ctx, orgID, email)
	if err != nil {
		return "", err
	}
	success, cancel := s.returnURLs()
	checkout, err := s.Bachs.SubscriptionCheckout(ctx, customerID, s.TeamProductID, success, cancel,
		map[string]string{"org_id": orgID, "plan": Team})
	if err != nil {
		return "", fmt.Errorf("team checkout: %w", err)
	}
	return checkout.URL, nil
}

/*
Cancel ends the current paid plan.

Team is cancelled at the end of the period already paid for, through Bachs;
the webhook moves the organization to free when it actually ends. Pay as you
go stops counting at once — the month so far is still charged at the start of
the next, because those people did have access.

Nobody's access is removed by either. Free only refuses the next addition.
*/
func (s Service) Cancel(ctx context.Context, orgID string) error {
	if !s.Enabled() {
		return errDisabled
	}
	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return err
	}
	switch {
	case a.teamActive():
		if a.subscriptionID == "" {
			return errors.New("no subscription id recorded for this organization")
		}
		if err := s.Bachs.CancelSubscription(ctx, a.subscriptionID); err != nil {
			return err
		}
		_, err := s.DB.Exec(ctx, `UPDATE billing_accounts SET cancel_at_period_end = true, updated_at = now() WHERE org_id = $1`, orgID)
		return err
	case a.Plan == PAYG:
		_, err := s.DB.Exec(ctx, `UPDATE billing_accounts SET plan = 'free', updated_at = now() WHERE org_id = $1`, orgID)
		return err
	}
	return errors.New("nothing to cancel: this organization is on the free plan")
}

// Portal returns a Bachs customer portal link for card updates and invoices.
func (s Service) Portal(ctx context.Context, orgID string) (string, error) {
	if !s.Enabled() {
		return "", errDisabled
	}
	a, err := account(ctx, s.DB, orgID)
	if err != nil {
		return "", err
	}
	if a.CustomerID == "" {
		return "", errors.New("no payment details yet; choose a plan first")
	}
	return s.Bachs.PortalURL(ctx, a.CustomerID)
}

// RetryNow lets an owner who has fixed their card try a failed month again
// without waiting for the schedule. It grants one more attempt.
func (s Service) RetryNow(ctx context.Context, orgID string) error {
	if !s.Enabled() {
		return errDisabled
	}
	tag, err := s.DB.Exec(ctx, `
		UPDATE payg_charges SET attempts = LEAST(attempts, $2 - 1), next_attempt_at = now(), updated_at = now()
		 WHERE org_id = $1 AND status = 'failed'
	`, orgID, MaxAttempts)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no failed charge to retry")
	}
	return s.retryDue(ctx)
}
