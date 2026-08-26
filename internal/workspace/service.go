package workspace

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Personal struct{ UserID, OrganizationID string }
type Service struct{ DB *pgxpool.Pool }

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
	if err = tx.QueryRow(ctx, `INSERT INTO users(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id`, email).Scan(&p.UserID); err != nil {
		return Personal{}, err
	}
	err = tx.QueryRow(ctx, `SELECT o.id FROM organizations o JOIN memberships m ON m.org_id=o.id WHERE m.user_id=$1 AND o.type='personal'`, p.UserID).Scan(&p.OrganizationID)
	if err == nil {
		return p, tx.Commit(ctx)
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
	return p, tx.Commit(ctx)
}
