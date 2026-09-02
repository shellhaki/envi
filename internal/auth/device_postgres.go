package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDeviceStore stores device authorizations in the device_authorizations
// table. Redeem uses SELECT ... FOR UPDATE so a code is redeemed exactly once
// even under concurrent polling, matching PostgresTokens.Take.
type PostgresDeviceStore struct{ DB *pgxpool.Pool }

func (p *PostgresDeviceStore) Create(deviceHash []byte, userCode string, expiresAt time.Time) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	_, e := p.DB.Exec(context.Background(),
		`INSERT INTO device_authorizations(device_code_hash,user_code,expires_at)VALUES($1,$2,$3)`,
		deviceHash, userCode, expiresAt)
	return e
}

func (p *PostgresDeviceStore) Approve(userCode, userID string) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	tag, e := p.DB.Exec(context.Background(),
		`UPDATE device_authorizations SET status='approved',user_id=$2,approved_at=now() WHERE user_code=$1 AND status='pending' AND expires_at>now()`,
		userCode, userID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

func (p *PostgresDeviceStore) Deny(userCode string) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	tag, e := p.DB.Exec(context.Background(),
		`UPDATE device_authorizations SET status='denied' WHERE user_code=$1 AND status='pending' AND expires_at>now()`,
		userCode)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

func (p *PostgresDeviceStore) Redeem(deviceHash []byte) (string, error) {
	if p == nil || p.DB == nil {
		return "", errors.New("database is required")
	}
	ctx := context.Background()
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var id, status string
	var userID *string
	var expiresAt time.Time
	// FOR UPDATE serialises concurrent polls: the first to see 'approved' flips
	// the row to 'redeemed', the rest fall through to expired_token below.
	e = tx.QueryRow(ctx, `SELECT id,status,user_id,expires_at FROM device_authorizations WHERE device_code_hash=$1 FOR UPDATE`, deviceHash).Scan(&id, &status, &userID, &expiresAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrDeviceNotFound
	}
	if e != nil {
		return "", e
	}
	if time.Now().After(expiresAt) {
		return "", DevicePending{"expired_token"}
	}
	switch status {
	case "pending":
		return "", DevicePending{"authorization_pending"}
	case "denied":
		return "", DevicePending{"access_denied"}
	case "approved":
		if userID == nil {
			return "", DevicePending{"authorization_pending"}
		}
		if _, e = tx.Exec(ctx, `UPDATE device_authorizations SET status='redeemed' WHERE id=$1`, id); e != nil {
			return "", e
		}
		if e = tx.Commit(ctx); e != nil {
			return "", e
		}
		return *userID, nil
	default: // redeemed or unexpected
		return "", DevicePending{"expired_token"}
	}
}
