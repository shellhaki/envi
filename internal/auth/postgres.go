package auth

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type PostgresTokens struct {
	DB         *pgxpool.Pool
	RefreshTTL time.Duration
}

func (p *PostgresTokens) Save(user, refresh, access string) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	_, e := p.DB.Exec(context.Background(), `INSERT INTO sessions(user_id,refresh_token_hash,access_token_hash,expires_at)VALUES($1,$2,$3,$4)`, user, HashToken(refresh), HashToken(access), time.Now().Add(p.RefreshTTL))
	return e
}
func (p *PostgresTokens) Take(refresh string) (string, string, error) {
	if p == nil || p.DB == nil {
		return "", "", errors.New("database is required")
	}
	tx, e := p.DB.Begin(context.Background())
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback(context.Background())
	var id, user string
	var expires time.Time
	e = tx.QueryRow(context.Background(), `SELECT id,user_id,expires_at FROM sessions WHERE refresh_token_hash=$1 AND revoked_at IS NULL FOR UPDATE`, HashToken(refresh)).Scan(&id, &user, &expires)
	if e != nil || time.Now().After(expires) {
		return "", "", errors.New("invalid refresh token")
	}
	access, e := token()
	if e != nil {
		return "", "", e
	}
	if _, e = tx.Exec(context.Background(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, id); e != nil {
		return "", "", e
	}
	if e = tx.Commit(context.Background()); e != nil {
		return "", "", e
	}
	return user, access, nil
}
func (p *PostgresTokens) Revoke(refresh string) error {
	_, e := p.DB.Exec(context.Background(), `UPDATE sessions SET revoked_at=now() WHERE refresh_token_hash=$1`, HashToken(refresh))
	return e
}
func (p *PostgresTokens) Authenticate(access string) (string, error) {
	var user string
	e := p.DB.QueryRow(context.Background(), `SELECT user_id FROM sessions WHERE access_token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, HashToken(access)).Scan(&user)
	if e != nil {
		return "", errors.New("invalid access token")
	}
	return user, nil
}
