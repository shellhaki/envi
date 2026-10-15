package billing

import (
	"errors"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/bachs"
	"shellhaki/envi/internal/config"
)

/*
New builds billing for this instance from its configuration.

Billing is on only on a hosted deployment with a valid Bachs key. Anything
less returns a Service with no Bachs client, which allows everything — and,
deliberately, does not fail startup, since that would take sign-in down with
it.

The one error is a live key on a developer's machine, which must not start at
all.
*/
func New(db *pgxpool.Pool, c config.Config, d config.Deployment) (Service, error) {
	s := Service{DB: db, WebURL: c.WebURL, ReturnURL: c.Bachs.ReturnURL,
		TeamProductID: c.Bachs.TeamProductID, WebhookSecret: c.Bachs.WebhookSecret}
	if !d.Billing() {
		return s, nil
	}

	client, err := bachs.New(c.Bachs.SecretKey)
	if err != nil {
		log.Printf("WARNING: billing is OFF: %v (hosted mode needs BACHS_SECRET_KEY)", err)
		return s, nil
	}
	// ENVIRONMENT alone can't tell a laptop from production: a .env copied
	// from production carries ENVIRONMENT=production. A local database or
	// dashboard is something production never has.
	if client.Live() && (c.Environment == "" || config.IsLocalURL(c.DatabaseURL) || config.IsLocalURL(c.WebURL)) {
		return s, errors.New("refusing to start: a live Bachs key (sk_live_) on a local instance; use an sk_sandbox_ key locally")
	}

	if !client.Live() && c.Environment == "production" {
		log.Printf("WARNING: production is using a sandbox Bachs key — no real money moves")
	}
	if s.WebhookSecret == "" {
		log.Printf("WARNING: BACHS_WEBHOOK_SECRET is unset — payments will be taken but never recorded")
	}
	if returnTo := s.returnBase(); config.IsLocalURL(returnTo) {
		log.Printf("WARNING: checkout would return to %s, which Bachs refuses. Set BACHS_RETURN_URL to a public URL", returnTo)
	}

	s.Bachs = client
	mode := "sandbox"
	if client.Live() {
		mode = "live"
	}
	log.Printf("billing on (%s)", mode)
	return s, nil
}
