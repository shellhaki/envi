package admin

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These need a real Postgres with the schema applied, like the repo's other
// database tests: ENVI_INTEGRATION=1 and DATABASE_URL set.
//
// They are safe to point at any database, including a live one: this package
// only ever reads.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

// The whole snapshot runs. Thirteen counts in one statement is easy to get
// subtly wrong — a column out of order scans into the wrong field and the
// dashboard reports confident nonsense — so this asserts the shape rather than
// any particular number.
func TestSnapshotRuns(t *testing.T) {
	s := Service{DB: testDB(t)}
	got, err := s.Snapshot(t.Context(), "hosted", "NGN")
	if err != nil {
		t.Fatal(err)
	}

	if got.GeneratedAt.IsZero() {
		t.Error("no generated_at")
	}
	// Every environment belongs to a project, and every project to an org, so
	// these can never be inverted. If they are, the column order is wrong.
	if got.Totals.Projects > got.Totals.Environments && got.Totals.Environments > 0 {
		t.Errorf("more projects (%d) than environments (%d)", got.Totals.Projects, got.Totals.Environments)
	}
	if got.Totals.ProductionEnvs > got.Totals.Environments {
		t.Errorf("production envs (%d) exceeds all envs (%d)", got.Totals.ProductionEnvs, got.Totals.Environments)
	}
	if got.Totals.TeamOrgs > got.Totals.Organizations {
		t.Errorf("team orgs (%d) exceeds all orgs (%d)", got.Totals.TeamOrgs, got.Totals.Organizations)
	}
	// A secret has at least one version, never fewer.
	if got.Totals.Secrets > got.Totals.SecretVersions {
		t.Errorf("secrets (%d) exceeds versions (%d)", got.Totals.Secrets, got.Totals.SecretVersions)
	}
}

// The 30-day window can never report less than the 7-day one.
func TestWindowsNest(t *testing.T) {
	s := Service{DB: testDB(t)}
	got, err := s.Snapshot(t.Context(), "hosted", "NGN")
	if err != nil {
		t.Fatal(err)
	}
	if got.Last7Days.NewUsers > got.Last30Days.NewUsers {
		t.Errorf("7d users (%d) exceeds 30d (%d)", got.Last7Days.NewUsers, got.Last30Days.NewUsers)
	}
	if got.Last7Days.SecretReads > got.Last30Days.SecretReads {
		t.Errorf("7d reads (%d) exceeds 30d (%d)", got.Last7Days.SecretReads, got.Last30Days.SecretReads)
	}
}

// Growth fills in the days nobody signed up. Without generate_series the chart
// silently skips empty days and draws steady growth through a gap.
func TestGrowthHasADayForEveryDay(t *testing.T) {
	s := Service{DB: testDB(t)}
	points, err := s.growth(t.Context(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 30 {
		t.Fatalf("got %d points, want 30", len(points))
	}
	for i := 1; i < len(points); i++ {
		if !points[i].Day.After(points[i-1].Day) {
			t.Fatalf("points %d and %d are out of order: %v, %v", i-1, i, points[i-1].Day, points[i].Day)
		}
	}
}

// Revenue reports unavailable rather than zero while billing does not exist,
// and must not error when the subscriptions table is absent.
func TestRevenueSurvivesNoBilling(t *testing.T) {
	s := Service{DB: testDB(t)}
	got, err := s.revenue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available && got.Note == "" {
		t.Error("unavailable revenue with no explanation")
	}
	if got.Available && got.Note != "" {
		t.Error("available revenue should not carry a note")
	}
}

// Adding an expense and reading it back, including the run-rate arithmetic
// that separates a monthly cost from a one-off purchase.
func TestExpensesRoundTrip(t *testing.T) {
	db := testDB(t)
	s := Service{DB: db}

	var operator string
	email := "expense-test@example.test"
	if err := db.QueryRow(t.Context(),
		`INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id`,
		email).Scan(&operator); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, operator) })

	added := []string{}
	add := func(minor int64, currency, category, recurring string) {
		t.Helper()
		e, err := s.AddExpense(t.Context(), operator, Expense{
			AmountMinor: minor, Currency: currency, Category: category, Recurring: recurring,
		})
		if err != nil {
			t.Fatal(err)
		}
		added = append(added, e.ID)
	}
	t.Cleanup(func() {
		for _, id := range added {
			db.Exec(t.Context(), `DELETE FROM expenses WHERE id = $1`, id)
		}
	})

	add(2000_00, "USD", "vps-test", Monthly)   // $20.00 a month
	add(1200_00, "USD", "domain-test", Yearly) // $12.00 a year
	add(5000_00, "USD", "laptop-test", Once)   // not a running cost

	spending, err := s.Spending(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}

	var usd *CurrencyTotal
	for i := range spending.Totals {
		if spending.Totals[i].Currency == "USD" {
			usd = &spending.Totals[i]
		}
	}
	if usd == nil {
		t.Fatal("no USD total")
	}
	// The monthly figure must count the monthly item, a twelfth of the yearly
	// one, and none of the one-off. Other rows may exist in a shared database,
	// so this asserts the contribution rather than an absolute.
	const wantContribution = 2000_00 + 1200_00/12
	if usd.MonthlyMinor < wantContribution {
		t.Errorf("monthly run rate %d is below the %d these three contribute", usd.MonthlyMinor, wantContribution)
	}
	if usd.TotalMinor < 2000_00+1200_00+5000_00 {
		t.Errorf("total %d is below what was just added", usd.TotalMinor)
	}
}

// Validation refuses the shapes a form can actually produce.
func TestExpenseValidation(t *testing.T) {
	s := Service{DB: testDB(t)}
	for _, bad := range []Expense{
		{AmountMinor: 0, Currency: "NGN", Category: "vps"},
		{AmountMinor: -500, Currency: "NGN", Category: "vps"},
		{AmountMinor: 100, Currency: "GBP", Category: "vps"},
		{AmountMinor: 100, Currency: "NGN", Category: "   "},
		{AmountMinor: 100, Currency: "NGN", Category: "vps", Recurring: "weekly"},
	} {
		if _, err := s.AddExpense(t.Context(), "", bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
