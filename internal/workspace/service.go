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
	// Created is true only on the sign-up that first made this account, so a
	// caller can greet somebody once rather than on every login.
	Created bool
}

type Service struct {
	DB *pgxpool.Pool
	// OnNewUser is called once, after the account is committed, for somebody
	// who has just been created. It is called for its side effects — a welcome
	// email — so it runs after the transaction rather than inside it, and its
	// failure is never allowed to fail the sign-up: somebody who cannot be
	// emailed has still signed up.
	OnNewUser func(email string)
}

func (s Service) Ensure(ctx context.Context, email string) error {
	_, err := s.Provision(ctx, email)
	return err
}
func (s Service) Identity(ctx context.Context, email string) (string, error) {
	p, err := s.Provision(ctx, email)
	return p.UserID, err
}

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
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, email); err != nil {
		return Personal{}, err
	}
	var p Personal
	// xmax is zero on a row this statement inserted and non-zero on one it
	// updated, which is how an upsert can report which of the two it did.
	// Without it there is no way to tell a sign-up from a sign-in here.
	if err = tx.QueryRow(ctx, `INSERT INTO users(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id, (xmax = 0)`, email).Scan(&p.UserID, &p.Created); err != nil {
		return Personal{}, err
	}
	err = tx.QueryRow(ctx, `SELECT o.id FROM organizations o JOIN memberships m ON m.org_id=o.id WHERE m.user_id=$1 AND o.type='personal'`, p.UserID).Scan(&p.OrganizationID)
	if err == nil {
		return p, s.commit(ctx, tx, p, email)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Personal{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO organizations(name,type) VALUES($1,'personal') RETURNING id`, email).Scan(&p.OrganizationID); err != nil {
		return Personal{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(user_id,org_id,role) VALUES($1,$2,'owner')`, p.UserID, p.OrganizationID); err != nil {
		return Personal{}, err
	}
	return p, s.commit(ctx, tx, p, email)
}

// commit finishes the transaction and, only if it succeeded and the account is
// new, announces it. Announcing before the commit would greet somebody whose
// sign-up then rolled back.
func (s Service) commit(ctx context.Context, tx pgx.Tx, p Personal, email string) error {
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if p.Created && s.OnNewUser != nil {
		s.OnNewUser(email)
	}
	return nil
}
