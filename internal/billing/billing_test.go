package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/bachs"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/workspace"
)

// fake is Bachs with no network: it records every charge so a test can assert
// exactly what would have been billed.
type fake struct {
	mu        sync.Mutex
	charges   []fakeCharge
	cancelled []string
	status    string // what ChargeSavedCard replies with; "processing" by default
}

type fakeCharge struct {
	customer string
	minor    int64
	currency string
	key      string
}

func (f *fake) CreateCustomer(ctx context.Context, email, name string, md map[string]string) (string, error) {
	return "cust_" + md["org_id"][:8], nil
}
func (f *fake) SaveCardCheckout(ctx context.Context, c, s, x string, md map[string]string) (bachs.Checkout, error) {
	return bachs.Checkout{ID: "chk_card", URL: "https://pay.test/card"}, nil
}
func (f *fake) SubscriptionCheckout(ctx context.Context, c, p, s, x string, md map[string]string) (bachs.Checkout, error) {
	return bachs.Checkout{ID: "chk_team", URL: "https://pay.test/team"}, nil
}
func (f *fake) CancelSubscription(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	return nil
}
func (f *fake) ChargeSavedCard(ctx context.Context, customer string, minor int64, currency, d, key string) (bachs.Charge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.charges = append(f.charges, fakeCharge{customer, minor, currency, key})
	status := f.status
	if status == "" {
		status = "processing"
	}
	return bachs.Charge{ID: fmt.Sprintf("ch_%d", len(f.charges)), Status: status}, nil
}
func (f *fake) PortalURL(ctx context.Context, c string) (string, error) {
	return "https://pay.test/portal", nil
}

func (f *fake) list() []fakeCharge {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCharge(nil), f.charges...)
}

// world is one owner with one organization and one project, on a real
// database.
type world struct {
	t       *testing.T
	db      *pgxpool.Pool
	s       Service
	bachs   *fake
	orgID   string
	project string
	ownerID string
	clock   time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	w := &world{t: t, db: db, bachs: &fake{}, clock: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
	owner, err := workspace.Service{DB: db}.Provision(t.Context(), fmt.Sprintf("bill-owner-%d@example.test", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	w.ownerID, w.orgID = owner.UserID, owner.OrganizationID
	t.Cleanup(func() {
		db.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, w.orgID)
		db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, w.ownerID)
	})
	p, err := project.Service{DB: db}.Create(t.Context(), w.ownerID, w.orgID, fmt.Sprintf("bill-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	w.project = p.ID
	w.s = Service{DB: db, Bachs: w.bachs, TeamProductID: "prod_team", WebURL: "https://envi.test", Now: func() time.Time { return w.clock }}
	return w
}

// collaborator gives a new person a grant on the world's project and returns
// their email.
func (w *world) collaborator() string {
	w.t.Helper()
	email := fmt.Sprintf("bill-collab-%d@example.test", time.Now().UnixNano())
	var id string
	if err := w.db.QueryRow(w.t.Context(), `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { w.db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
	if _, err := w.db.Exec(w.t.Context(), `INSERT INTO access_grants (subject_user_id, project_id, permission) VALUES ($1, $2, 'read')`, id, w.project); err != nil {
		w.t.Fatal(err)
	}
	return email
}

func (w *world) set(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.db.Exec(w.t.Context(), `INSERT INTO billing_accounts (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, w.orgID); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.db.Exec(w.t.Context(), `UPDATE billing_accounts SET `+sql+` WHERE org_id = $1`, append([]any{w.orgID}, args...)...); err != nil {
		w.t.Fatal(err)
	}
}

func refusalCode(err error) string {
	if r, ok := IsRefusal(err); ok {
		return r.Code
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// A self-hosted instance has no Bachs, and allows everything.
func TestDisabledAllowsEverything(t *testing.T) {
	w := newWorld(t)
	w.s.Bachs = nil
	if err := w.s.AllowNewPerson(t.Context(), w.orgID, "anyone@example.test"); err != nil {
		t.Fatalf("a disabled instance refused: %v", err)
	}
}

func TestPlansDecideWhoMayBeAdded(t *testing.T) {
	w := newWorld(t)
	newcomer := "bill-new@example.test"

	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "payment_required" {
		t.Errorf("free: got %q, want payment_required", got)
	}

	w.set(`plan = 'payg'`)
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "payment_required" {
		t.Errorf("payg without a card: got %q, want payment_required", got)
	}

	w.set(`card_saved = true, bachs_customer_id = $2`, "cust_"+w.orgID[:8])
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "" {
		t.Errorf("payg with a card: got %q, want allowed", got)
	}

	w.set(`payg_cap = 1`)
	w.collaborator()
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "payg_cap_reached" {
		t.Errorf("payg at cap: got %q, want payg_cap_reached", got)
	}

	w.set(`plan = 'team', subscription_status = 'active'`)
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "" {
		t.Errorf("team: got %q, want allowed", got)
	}

	w.set(`subscription_status = 'canceled'`)
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, newcomer)); got != "payment_required" {
		t.Errorf("team cancelled: got %q, want payment_required", got)
	}
}

// Somebody already in costs nothing to add to another project, even on free.
func TestAddingSomebodyAlreadyInIsFree(t *testing.T) {
	w := newWorld(t)
	existing := w.collaborator()
	if err := w.s.AllowNewPerson(t.Context(), w.orgID, existing); err != nil {
		t.Fatalf("refused somebody already counted: %v", err)
	}
	var owner string
	w.db.QueryRow(t.Context(), `SELECT email FROM users WHERE id = $1`, w.ownerID).Scan(&owner)
	if err := w.s.AllowNewPerson(t.Context(), w.orgID, strings.ToUpper(owner)); err != nil {
		t.Fatalf("refused the owner: %v", err)
	}
}

// A person on three projects is one person.
func TestPeopleAreCountedOnce(t *testing.T) {
	w := newWorld(t)
	email := w.collaborator()
	for i := 0; i < 2; i++ {
		p, err := project.Service{DB: w.db}.Create(t.Context(), w.ownerID, w.orgID, fmt.Sprintf("bill-extra-%d-%d", i, time.Now().UnixNano()))
		if err != nil {
			t.Fatal(err)
		}
		w.db.Exec(t.Context(), `INSERT INTO access_grants (subject_user_id, project_id, permission) SELECT id, $2, 'read' FROM users WHERE email = $1`, email, p.ID)
	}
	n, err := w.s.extraCount(t.Context(), w.db, w.orgID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("one person on three projects counted as %d", n)
	}
}

func (w *world) onPAYG(currency string) {
	w.set(`plan = 'payg', currency = $2, card_saved = true, bachs_customer_id = $3`, currency, "cust_"+w.orgID[:8])
}

func (w *world) pass() {
	w.t.Helper()
	if err := w.s.Pass(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) charge(month string) (status string, amount, carried int64, attempts int) {
	w.t.Helper()
	err := w.db.QueryRow(w.t.Context(), `
		SELECT status, amount_minor, carried_minor, attempts FROM payg_charges WHERE org_id = $1 AND month = $2::date
	`, w.orgID, month+"-01").Scan(&status, &amount, &carried, &attempts)
	if err != nil {
		w.t.Fatalf("no charge row for %s: %v", month, err)
	}
	return
}

/*
A dollar month too small to be worth charging is carried, and paid with the
first month that is.

One person in September is $0.40, which Bachs's fixed $0.40 fee would swallow,
so September is carried. Seven in October is $2.80; with September's $0.40
that is $3.20, over the $3 minimum, so October is charged $3.20 — once.
*/
func TestSmallDollarMonthsAreCarried(t *testing.T) {
	w := newWorld(t)
	w.onPAYG("USD")
	w.collaborator()
	w.pass() // counts September

	w.clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	w.pass() // closes September
	if status, amount, _, _ := w.charge("2026-09"); status != "carried" || amount != 40 {
		t.Fatalf("September: %s %d, want carried 40", status, amount)
	}
	if n := len(w.bachs.list()); n != 0 {
		t.Fatalf("charged %d time(s) for a carried month", n)
	}

	for i := 0; i < 6; i++ {
		w.collaborator()
	}
	w.pass() // counts all seven in October

	w.clock = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	w.pass()
	w.pass() // a second pass must not charge again

	charges := w.bachs.list()
	if len(charges) != 1 {
		t.Fatalf("charged %d times, want 1: %+v", len(charges), charges)
	}
	if charges[0].minor != 320 || charges[0].currency != "USD" {
		t.Fatalf("charged %d %s, want 320 USD", charges[0].minor, charges[0].currency)
	}
	if want := "payg:" + w.orgID + ":2026-10:1"; charges[0].key != want {
		t.Fatalf("key %q, want %q", charges[0].key, want)
	}
	if status, _, _, _ := w.charge("2026-09"); status != "settled" {
		t.Fatalf("September is %s after October paid it, want settled", status)
	}
	if status, amount, carried, _ := w.charge("2026-10"); status != "processing" || amount != 280 || carried != 40 {
		t.Fatalf("October: %s %d+%d, want processing 280+40", status, amount, carried)
	}
}

// Naira has no fixed fee, so even one person is charged at once.
func TestNairaMonthsAreAlwaysCharged(t *testing.T) {
	w := newWorld(t)
	w.onPAYG("NGN")
	w.collaborator()
	w.pass()
	w.clock = time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	w.pass()

	charges := w.bachs.list()
	if len(charges) != 1 || charges[0].minor != 60000 || charges[0].currency != "NGN" {
		t.Fatalf("charges %+v, want one of 60000 NGN", charges)
	}
}

// Somebody removed before the month ends still counts for it.
func TestRemovedPeopleStillCountForTheirMonth(t *testing.T) {
	w := newWorld(t)
	w.onPAYG("NGN")
	email := w.collaborator()
	w.pass()
	w.db.Exec(t.Context(), `DELETE FROM access_grants WHERE subject_user_id = (SELECT id FROM users WHERE email = $1)`, email)

	w.clock = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	w.pass()
	if _, amount, _, _ := w.charge("2026-09"); amount != 60000 {
		t.Fatalf("September billed %d, want 60000 for the person removed mid-month", amount)
	}
}

func signed(t *testing.T, id, typ string, created time.Time, data map[string]any) bachs.Event {
	t.Helper()
	// Applying an event records its id; remove it afterwards so test events
	// do not pile up among real ones in a shared database.
	t.Cleanup(func() {
		if db, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL")); err == nil {
			db.Exec(context.Background(), `DELETE FROM bachs_events WHERE id = $1`, id)
			db.Close()
		}
	})
	raw, _ := json.Marshal(data)
	return bachs.Event{ID: id, Type: typ, CreatedAt: created, Data: raw}
}

func TestCollectionWebhooksSettleAndRetry(t *testing.T) {
	w := newWorld(t)
	w.onPAYG("NGN")
	w.collaborator()
	w.pass()
	w.clock = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	w.pass()

	ref := "payg:" + w.orgID + ":2026-09:1"
	failed := signed(t, "evt_fail_"+w.orgID[:8], "collection.failed", w.clock, map[string]any{"reference": ref, "reason": "Insufficient funds"})
	if err := w.s.Apply(t.Context(), failed); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := w.charge("2026-09"); status != "failed" {
		t.Fatalf("after collection.failed: %s", status)
	}
	// The same event again changes nothing and does not error.
	if err := w.s.Apply(t.Context(), failed); err != nil {
		t.Fatalf("replaying an applied event: %v", err)
	}

	// Not due yet: nothing is sent.
	w.pass()
	if n := len(w.bachs.list()); n != 1 {
		t.Fatalf("retried early: %d charges", n)
	}
	// Three days later the second attempt goes out, with a new key.
	w.clock = w.clock.Add(3*24*time.Hour + time.Minute)
	w.pass()
	charges := w.bachs.list()
	if len(charges) != 2 || !strings.HasSuffix(charges[1].key, ":2") {
		t.Fatalf("retry: %+v", charges)
	}

	// A late success for the first attempt must not mark the second paid.
	late := signed(t, "evt_late_"+w.orgID[:8], "collection.succeeded", w.clock, map[string]any{"reference": ref})
	w.s.Apply(t.Context(), late)
	if status, _, _, _ := w.charge("2026-09"); status != "processing" {
		t.Fatalf("a stale success changed the month to %s", status)
	}

	ok := signed(t, "evt_ok_"+w.orgID[:8], "collection.succeeded", w.clock, map[string]any{"reference": "payg:" + w.orgID + ":2026-09:2", "charge_id": "ch_2"})
	if err := w.s.Apply(t.Context(), ok); err != nil {
		t.Fatal(err)
	}
	if status, _, _, attempts := w.charge("2026-09"); status != "succeeded" || attempts != 2 {
		t.Fatalf("after success: %s after %d attempts", status, attempts)
	}
}

// After the last attempt fails the organization is past due, and may not add
// anybody until it pays.
func TestRunningOutOfRetriesIsPastDue(t *testing.T) {
	w := newWorld(t)
	w.onPAYG("NGN")
	w.bachs.status = "failed"
	w.collaborator()
	w.pass()

	w.clock = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	w.pass()
	w.clock = w.clock.Add(4 * 24 * time.Hour)
	w.pass()
	w.clock = w.clock.Add(8 * 24 * time.Hour)
	w.pass()
	w.clock = w.clock.Add(30 * 24 * time.Hour)
	w.pass() // no fourth attempt

	// Count attempts on September itself. October is charged separately and
	// legitimately: the person was still there, past due or not.
	september := 0
	for _, c := range w.bachs.list() {
		if strings.Contains(c.key, ":2026-09:") {
			september++
		}
	}
	if september != MaxAttempts {
		t.Fatalf("September attempted %d times, want %d", september, MaxAttempts)
	}
	if status, _, _, attempts := w.charge("2026-09"); status != "failed" || attempts != MaxAttempts {
		t.Fatalf("September is %s after %d attempts", status, attempts)
	}
	if got := refusalCode(w.s.AllowNewPerson(t.Context(), w.orgID, "bill-late@example.test")); got != "payg_past_due" {
		t.Fatalf("past due: got %q", got)
	}
}

func TestSubscriptionEvents(t *testing.T) {
	w := newWorld(t)
	md := map[string]any{"org_id": w.orgID}
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	created := signed(t, "evt_sc_"+w.orgID[:8], "customer.subscription.created", t0, map[string]any{
		"subscription_id": "sub_1", "product_id": "prod_team", "status": "active", "metadata": md,
	})
	if err := w.s.Apply(t.Context(), created); err != nil {
		t.Fatal(err)
	}
	a, _ := account(t.Context(), w.db, w.orgID)
	if a.Plan != Team || a.SubscriptionStatus != "active" {
		t.Fatalf("after created: %+v", a)
	}

	deleted := signed(t, "evt_sd_"+w.orgID[:8], "customer.subscription.deleted", t0.Add(2*time.Hour), map[string]any{
		"subscription_id": "sub_1", "product_id": "prod_team", "metadata": md,
	})
	stale := signed(t, "evt_su_"+w.orgID[:8], "customer.subscription.updated", t0.Add(time.Hour), map[string]any{
		"subscription_id": "sub_1", "product_id": "prod_team", "status": "active", "metadata": md,
	})
	w.s.Apply(t.Context(), deleted)
	w.s.Apply(t.Context(), stale) // older than deleted, arriving after it

	a, _ = account(t.Context(), w.db, w.orgID)
	if a.Plan != Free {
		t.Fatalf("a late 'active' resurrected a cancelled subscription: %+v", a)
	}

	// A subscription for some other product is not Team.
	other := signed(t, "evt_so_"+w.orgID[:8], "customer.subscription.created", t0.Add(3*time.Hour), map[string]any{
		"subscription_id": "sub_2", "product_id": "prod_other", "status": "active", "metadata": md,
	})
	w.s.Apply(t.Context(), other)
	if a, _ = account(t.Context(), w.db, w.orgID); a.Plan != Free {
		t.Fatalf("another product's subscription made the org Team")
	}
}

// Saving a card through the pay-as-you-go checkout switches the plan, in the
// chosen currency, and counts everybody already there.
func TestCardCheckoutStartsPAYG(t *testing.T) {
	w := newWorld(t)
	w.collaborator()
	done := signed(t, "evt_cc_"+w.orgID[:8], "checkout.completed", w.clock, map[string]any{
		"mode": "setup", "customer": "cust_x",
		"metadata": map[string]string{"org_id": w.orgID, "plan": "payg", "currency": "NGN"},
	})
	if err := w.s.Apply(t.Context(), done); err != nil {
		t.Fatal(err)
	}
	a, _ := account(t.Context(), w.db, w.orgID)
	if a.Plan != PAYG || a.Currency != "NGN" || !a.CardSaved {
		t.Fatalf("after checkout: %+v", a)
	}
	var counted int
	w.db.QueryRow(t.Context(), `SELECT count(*) FROM billable_members WHERE org_id = $1`, w.orgID).Scan(&counted)
	if counted != 1 {
		t.Fatalf("counted %d people at switch, want 1", counted)
	}
}

// An event about nobody Envi knows is acknowledged, not retried forever.
func TestUnmatchedEventsAreAcknowledged(t *testing.T) {
	w := newWorld(t)
	stray := signed(t, "evt_stray_"+w.orgID[:8], "payment_method.saved", w.clock, map[string]any{"customer_id": "cust_nobody"})
	if err := w.s.Apply(t.Context(), stray); err != nil {
		t.Fatalf("unmatched event errored, which would make Bachs retry it forever: %v", err)
	}
}

// The return address falls back to the dashboard, and the override wins. The
// override exists because Bachs refuses localhost, which is what the dashboard
// is during local testing.
func TestReturnURLs(t *testing.T) {
	s := Service{WebURL: "http://localhost:3000/"}
	if ok, _ := s.returnURLs(); ok != "http://localhost:3000/billing/success" {
		t.Errorf("fallback success URL = %q", ok)
	}
	s.ReturnURL = "https://envisecrets.com"
	ok, cancel := s.returnURLs()
	if ok != "https://envisecrets.com/billing/success" || cancel != "https://envisecrets.com/dashboard/billing?checkout=cancelled" {
		t.Errorf("override: %q %q", ok, cancel)
	}
}
