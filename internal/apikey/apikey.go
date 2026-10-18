// Package apikey manages personal API keys, which a user exchanges for a session instead of signing in through a browser.
package apikey

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
)

// Keys live in api_tokens with user_id set (service tokens use service_identity_id instead).

// Prefix makes a leaked key easy to recognise and to scan for.
const Prefix = "envi_"

const DefaultLifetime = 90 * 24 * time.Hour

var ErrNotFound = errors.New("api key not found")

// Key is one API key. Secret is only filled in by Create; it is never stored.
type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Permission string     `json:"permission"`
	Secret     string     `json:"secret,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Create makes a new key. A lifetime of 0 never expires.
func Create(ctx context.Context, db *pgxpool.Pool, userID, name, permission string, lifetime time.Duration) (Key, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Key{}, errors.New("a name is required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Key{}, errors.New(`permission must be "read", "write" or "manage"`)
	}
	secret, err := newSecret()
	if err != nil {
		return Key{}, err
	}
	var expiresAt *time.Time
	if lifetime != 0 {
		t := time.Now().Add(lifetime)
		expiresAt = &t
	}

	key := Key{Name: name, Permission: permission, Secret: secret, ExpiresAt: expiresAt}
	err = db.QueryRow(ctx, `
		INSERT INTO api_tokens (user_id, token_hash, name, permission, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		userID, auth.HashToken(secret), name, permission, expiresAt).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return Key{}, err
	}
	return key, nil
}

// List returns the user's keys, newest first. Revoked keys are left out; expired ones are kept.
func List(ctx context.Context, db *pgxpool.Pool, userID string) ([]Key, error) {
	rows, err := db.Query(ctx, `
		SELECT id, coalesce(name, ''), permission, expires_at, last_used_at, created_at
		  FROM api_tokens
		 WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []Key{}
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Name, &k.Permission, &k.ExpiresAt, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Revoke stops a key working and ends every session started from it.
func Revoke(ctx context.Context, db *pgxpool.Pool, userID, keyID string) error {
	result, err := db.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, userID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" { // not a valid uuid
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = db.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE api_key_id = $1 AND revoked_at IS NULL`, keyID)
	return err
}

// Authenticate returns whose key it is and what it may do, and records that it was used.
func Authenticate(ctx context.Context, db *pgxpool.Pool, secret string) (userID, permission, keyID string, err error) {
	err = db.QueryRow(ctx, `
		SELECT user_id, permission, id FROM api_tokens
		 WHERE token_hash = $1 AND user_id IS NOT NULL AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > now())`,
		auth.HashToken(secret)).Scan(&userID, &permission, &keyID)
	if err != nil {
		return "", "", "", errors.New("invalid api key")
	}
	_, _ = db.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, keyID)
	return userID, permission, keyID, nil
}

func newSecret() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return Prefix + hex.EncodeToString(random), nil
}
