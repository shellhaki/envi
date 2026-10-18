package workspace

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Personal struct {
	UserID, OrganizationID string
	Created                bool // true only on the sign-in that created the account
}

type Service struct {
	DB *pgxpool.Pool
	// OnNewUser runs after a new account is saved, e.g. to send a welcome email.
	OnNewUser func(email string)
}

// Identity returns the user ID for an email, creating the account if needed.
func (s Service) Identity(ctx context.Context, email string) (string, error) {
	p, err := s.Provision(ctx, email)
	return p.UserID, err
}

// Provision finds or creates the user and their personal organization.
func (s Service) Provision(ctx context.Context, email string) (Personal, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || s.DB == nil {
		return Personal{}, errors.New("email and database are required")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Personal{}, err
	}
	defer tx.Rollback(ctx)

	// Two sign-ins for the same email at once would otherwise create two organizations.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, email); err != nil {
		return Personal{}, err
	}

	var p Personal
	// xmax = 0 is true when this statement inserted the row, false when it updated an existing one.
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email) VALUES ($1)
		    ON CONFLICT (email) DO UPDATE SET email = excluded.email
		RETURNING id, (xmax = 0)`, email).Scan(&p.UserID, &p.Created)
	if err != nil {
		return Personal{}, err
	}

	err = tx.QueryRow(ctx, `
		SELECT o.id FROM organizations o JOIN memberships m ON m.org_id = o.id
		 WHERE m.user_id = $1 AND o.type = 'personal'`, p.UserID).Scan(&p.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO organizations (name, type) VALUES ($1, 'personal') RETURNING id`,
			email).Scan(&p.OrganizationID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, 'owner')`,
				p.UserID, p.OrganizationID)
		}
	}
	if err != nil {
		return Personal{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Personal{}, err
	}
	if p.Created && s.OnNewUser != nil {
		s.OnNewUser(email)
	}
	return p, nil
}
