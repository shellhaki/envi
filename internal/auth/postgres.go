package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTokens stores sessions in the sessions table. Access and refresh
// tokens expire independently: access_expires_at bounds the bearer token used on
// every request, expires_at bounds how long the session may be refreshed for.
type PostgresTokens struct {
	DB                    *pgxpool.Pool
	AccessTTL, RefreshTTL time.Duration
}

func (p *PostgresTokens) Save(user, refresh, access string) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	// A session is only useful if it can be refreshed, so RefreshTTL must be
	// positive. AccessTTL is intentionally unguarded: a non-positive value mints
	// an already-expired access token (access_expires_at in the past) while the
	// session stays refreshable, per the independent-expiry design above.
	if p.RefreshTTL <= 0 {
		return errors.New("refresh TTL is required")
	}
	_, e := p.DB.Exec(context.Background(),
		`INSERT INTO sessions(user_id,refresh_token_hash,access_token_hash,access_expires_at,expires_at)VALUES($1,$2,$3,now()+make_interval(secs=>$4),now()+make_interval(secs=>$5))`,
		user, HashToken(refresh), HashToken(access), p.AccessTTL.Seconds(), p.RefreshTTL.Seconds())
	return e
}

func (p *PostgresTokens) Take(refresh string) (string, error) {
	if p == nil || p.DB == nil {
		return "", errors.New("database is required")
	}
	ctx := context.Background()
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var id, user string
	// FOR UPDATE serialises concurrent refreshes of the same token: the first
	// caller revokes the row, the rest fall through to "invalid refresh token".
	e = tx.QueryRow(ctx, `SELECT id,user_id FROM sessions WHERE refresh_token_hash=$1 AND revoked_at IS NULL AND expires_at>now() FOR UPDATE`, HashToken(refresh)).Scan(&id, &user)
	if e != nil {
		return "", errors.New("invalid refresh token")
	}
	if _, e = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, id); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return user, nil
}

func (p *PostgresTokens) Revoke(refresh string) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	_, e := p.DB.Exec(context.Background(), `UPDATE sessions SET revoked_at=now() WHERE refresh_token_hash=$1`, HashToken(refresh))
	return e
}

func (p *PostgresTokens) Authenticate(access string) (string, error) {
	if p == nil || p.DB == nil {
		return "", errors.New("database is required")
	}
	var user string
	e := p.DB.QueryRow(context.Background(), `SELECT user_id FROM sessions WHERE access_token_hash=$1 AND revoked_at IS NULL AND access_expires_at>now()`, HashToken(access)).Scan(&user)
	if e != nil {
		return "", errors.New("invalid access token")
	}
	return user, nil
}
