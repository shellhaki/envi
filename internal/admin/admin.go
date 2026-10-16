// Package admin reports what the whole instance is doing: how many people use
// it, how much they store, and how much of it is being paid for.
//
// It reads across every organization, which no other package is allowed to do,
// so two rules hold everywhere in here:
//
//   - It counts. It never returns a secret's value, a ciphertext, a nonce or a
//     token. The one text column it reads is users.email, because an operator
//     looking at a sign-up list needs to know who signed up.
//   - It is read-only. There is no write path in this package and there should
//     never be one; an admin screen that can also change things is an admin
//     screen that can be tricked into changing things.
package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ DB *pgxpool.Pool }

// Totals is the instance at a glance.
type Totals struct {
	Users          int64 `json:"users"`
	Organizations  int64 `json:"organizations"`
	TeamOrgs       int64 `json:"team_orgs"`
	Projects       int64 `json:"projects"`
	Environments   int64 `json:"environments"`
	ProductionEnvs int64 `json:"production_envs"`
	Secrets        int64 `json:"secrets"`
	SecretVersions int64 `json:"secret_versions"`
	KVEntries      int64 `json:"kv_entries"`
	Grants         int64 `json:"grants"`
	PendingInvites int64 `json:"pending_invites"`
	ServiceTokens  int64 `json:"service_tokens"`
	APIKeys        int64 `json:"api_keys"`
}

// Recent counts the same things again over a window, which is what says
// whether the instance is growing or just large.
type Recent struct {
	NewUsers     int64 `json:"new_users"`
	NewProjects  int64 `json:"new_projects"`
	SecretWrites int64 `json:"secret_writes"`
	SecretReads  int64 `json:"secret_reads"`
	ActiveOrgs   int64 `json:"active_orgs"`
	ActivePeople int64 `json:"active_people"`
}

// Revenue is what the instance is being paid, read from billing's own tables.
// It is reported as unavailable when those tables do not exist, rather than as
// zero: "nobody has paid" and "there is no way to pay" are different facts and
// an operator needs to tell them apart.
type Revenue struct {
	Available bool   `json:"available"`
	Note      string `json:"note,omitempty"`
	// MRR is what Team subscriptions bring in a month, in US dollars. Pay as
	// you go has no recurring amount, so it is reported as Collected instead.
	MRR       float64 `json:"mrr"`
	PaidOrgs  int64   `json:"paid_orgs"`
	FreeOrgs  int64   `json:"free_orgs"`
	TrialOrgs int64   `json:"trial_orgs"`
	TeamOrgs  int64   `json:"team_orgs"`
	PAYGOrgs  int64   `json:"payg_orgs"`
	// Collected is pay as you go money actually taken, per currency, since
	// no exchange rate is applied anywhere. Team payments are taken by Bachs on
	// its own schedule and are not recorded here, which is why they are MRR.
	Collected []Collected `json:"collected"`
	// FailedCharges is pay as you go charges currently refused and waiting
	// for a retry: money owed that has not arrived.
	FailedCharges int64 `json:"failed_charges"`
}

// Collected is pay as you go revenue in one currency, in minor units.
type Collected struct {
	Currency       string `json:"currency"`
	ThisMonthMinor int64  `json:"this_month_minor"`
	AllTimeMinor   int64  `json:"all_time_minor"`
}

// Signup is one person, for the list of who has joined.
type Signup struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Projects  int64     `json:"projects"`
	Secrets   int64     `json:"secrets"`
}

// Point is one day on a growth chart.
type Point struct {
	Day   time.Time `json:"day"`
	Users int64     `json:"users"`
}

// Snapshot is everything the admin screen shows, in one round trip. The screen
// has no partial states to design for and no loading spinners per card.
type Snapshot struct {
	GeneratedAt time.Time `json:"generated_at"`
	Mode        string    `json:"mode"`
	Currency    string    `json:"currency"`
	Totals      Totals    `json:"totals"`
	Last7Days   Recent    `json:"last_7_days"`
	Last30Days  Recent    `json:"last_30_days"`
	Revenue     Revenue   `json:"revenue"`
	Spending    Spending  `json:"spending"`
	Signups     []Signup  `json:"signups"`
	Growth      []Point   `json:"growth"`
}

// Snapshot takes the deployment so the screen can say which instance it is
// looking at. A self-hosted instance has no revenue to report and should not
// be shown an empty revenue card as though it were a problem.
func (s Service) Snapshot(ctx context.Context, mode, currency string) (Snapshot, error) {
	out := Snapshot{GeneratedAt: time.Now().UTC(), Mode: mode, Currency: currency}
	var err error
	if out.Totals, err = s.totals(ctx); err != nil {
		return out, err
	}
	if out.Last7Days, err = s.recent(ctx, 7); err != nil {
		return out, err
	}
	if out.Last30Days, err = s.recent(ctx, 30); err != nil {
		return out, err
	}
	if out.Revenue, err = s.revenue(ctx); err != nil {
		return out, err
	}
	if out.Spending, err = s.Spending(ctx, 25); err != nil {
		return out, err
	}
	if out.Signups, err = s.signups(ctx, 50); err != nil {
		return out, err
	}
	if out.Growth, err = s.growth(ctx, 30); err != nil {
		return out, err
	}
	return out, nil
}

// totals runs as one statement rather than thirteen. At this size it hardly
// matters for speed; it matters because every number then describes the same
// instant, and a dashboard whose rows disagree with each other is worse than
// one that is slightly slow.
func (s Service) totals(ctx context.Context) (Totals, error) {
	var t Totals
	err := s.DB.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM users),
		  (SELECT count(*) FROM organizations),
		  (SELECT count(*) FROM organizations WHERE type = 'team'),
		  (SELECT count(*) FROM projects),
		  (SELECT count(*) FROM environments),
		  (SELECT count(*) FROM environments WHERE is_production),
		  -- Deleted secrets are a soft delete; they are not stored any more
		  -- as far as the user is concerned, so they are not counted here.
		  (SELECT count(*) FROM secrets WHERE deleted_at IS NULL),
		  (SELECT count(*) FROM secret_versions),
		  (SELECT count(*) FROM kv_entries),
		  (SELECT count(*) FROM access_grants),
		  (SELECT count(*) FROM invitations WHERE status = 'pending'),
		  (SELECT count(*) FROM service_identities),
		  (SELECT count(*) FROM api_tokens WHERE user_id IS NOT NULL AND revoked_at IS NULL)
	`).Scan(
		&t.Users, &t.Organizations, &t.TeamOrgs, &t.Projects, &t.Environments,
		&t.ProductionEnvs, &t.Secrets, &t.SecretVersions, &t.KVEntries,
		&t.Grants, &t.PendingInvites, &t.ServiceTokens, &t.APIKeys,
	)
	return t, err
}

func (s Service) recent(ctx context.Context, days int) (Recent, error) {
	var r Recent
	since := time.Now().UTC().AddDate(0, 0, -days)
	err := s.DB.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM users WHERE created_at >= $1),
		  (SELECT count(*) FROM projects WHERE created_at >= $1),
		  (SELECT count(*) FROM audit_events WHERE created_at >= $1 AND action IN ('secret.write','secret.delete')),
		  (SELECT count(*) FROM audit_events WHERE created_at >= $1 AND action = 'secret.read'),
		  (SELECT count(DISTINCT org_id) FROM audit_events WHERE created_at >= $1),
		  (SELECT count(DISTINCT actor_id) FROM audit_events WHERE created_at >= $1 AND actor_id IS NOT NULL)
	`, since).Scan(&r.NewUsers, &r.NewProjects, &r.SecretWrites, &r.SecretReads, &r.ActiveOrgs, &r.ActivePeople)
	return r, err
}

// teamPriceMinor mirrors billing.TeamPriceMinor. It is repeated rather than
// imported because internal/billing depends on configuration this package has
// no reason to know about.
const teamPriceMinor = 400

// revenue reads billing_accounts and payg_charges. It survives those tables not
// existing, so the screen works on an instance that has not run migration 011.
func (s Service) revenue(ctx context.Context) (Revenue, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT to_regclass('public.billing_accounts') IS NOT NULL
		AND to_regclass('public.payg_charges') IS NOT NULL`).Scan(&exists); err != nil {
		return Revenue{}, err
	}
	if !exists {
		var orgs int64
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs); err != nil {
			return Revenue{}, err
		}
		return Revenue{
			Available: false,
			Note:      "Billing tables are missing on this database. Run migrations/011_billing.sql.",
			FreeOrgs:  orgs,
			Collected: []Collected{},
		}, nil
	}

	r := Revenue{Available: true, Collected: []Collected{}}
	err := s.DB.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM billing_accounts WHERE plan = 'team'),
		  (SELECT count(*) FROM billing_accounts WHERE plan = 'payg'),
		  (SELECT count(*) FROM billing_accounts WHERE subscription_status = 'trialing'),
		  (SELECT count(*) FROM organizations),
		  (SELECT count(*) FROM payg_charges WHERE status = 'failed')
	`).Scan(&r.TeamOrgs, &r.PAYGOrgs, &r.TrialOrgs, &r.FreeOrgs, &r.FailedCharges)
	if err != nil {
		return r, err
	}
	r.PaidOrgs = r.TeamOrgs + r.PAYGOrgs
	r.FreeOrgs -= r.PaidOrgs
	r.MRR = float64(r.TeamOrgs*teamPriceMinor) / 100

	// A succeeded charge carries its own month plus any small months carried
	// into it. It counts toward the month it was paid in, not the month it
	// was for.
	rows, err := s.DB.Query(ctx, `
		SELECT currency,
		       coalesce(sum(amount_minor + carried_minor) FILTER (WHERE updated_at >= date_trunc('month', now())), 0),
		       coalesce(sum(amount_minor + carried_minor), 0)
		  FROM payg_charges
		 WHERE status = 'succeeded'
		 GROUP BY currency
		 ORDER BY currency`)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Collected
		if err := rows.Scan(&c.Currency, &c.ThisMonthMinor, &c.AllTimeMinor); err != nil {
			return r, err
		}
		r.Collected = append(r.Collected, c)
	}
	return r, rows.Err()
}

// signups lists who joined, newest first, with how much each one actually
// uses. A sign-up that never created a project is the most important number on
// an early dashboard and it does not show up in any total.
func (s Service) signups(ctx context.Context, limit int) ([]Signup, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT u.email, coalesce(u.name, ''), u.created_at,
		       (SELECT count(*) FROM projects p
		          JOIN memberships m ON m.org_id = p.org_id
		         WHERE m.user_id = u.id),
		       (SELECT count(*) FROM secrets s
		          JOIN environments e ON e.id = s.environment_id
		          JOIN projects p ON p.id = e.project_id
		          JOIN memberships m ON m.org_id = p.org_id
		         WHERE m.user_id = u.id AND s.deleted_at IS NULL)
		  FROM users u
		 ORDER BY u.created_at DESC
		 LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Signup{}
	for rows.Next() {
		var s Signup
		if err := rows.Scan(&s.Email, &s.Name, &s.CreatedAt, &s.Projects, &s.Secrets); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// growth returns one row per day, including the days nobody signed up —
// generate_series fills those in, because a chart that silently skips empty
// days draws a flat line through a gap and reads as steady growth.
func (s Service) growth(ctx context.Context, days int) ([]Point, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT d::date, coalesce(c.n, 0)
		  FROM generate_series(current_date - ($1::int - 1), current_date, '1 day') AS d
		  LEFT JOIN (
		    SELECT created_at::date AS day, count(*) AS n
		      FROM users
		     WHERE created_at >= current_date - ($1::int - 1)
		     GROUP BY 1
		  ) c ON c.day = d::date
		 ORDER BY d
	`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Day, &p.Users); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
