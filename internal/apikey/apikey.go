// Package apikey is personal API keys: a long-lived secret a user creates once
// and uses instead of logging in through a browser.
//
// A key is not a way of making requests. It is a way of *starting a session*:
// you hand the key to POST /auth/api-key, get an ordinary access and refresh
// token back, and use those. That keeps one kind of credential on the wire and
// means a key can be revoked without hunting down what it produced.
//
// A key may be capped at read, write or manage. Exchanging it produces a session
// carrying the same cap, so the ceiling survives refreshes.
//
// Keys live in api_tokens, the same table as service tokens: a row with user_id
// set is a personal key, a row with service_identity_id set is a machine token,
// and the table's CHECK constraint makes sure it's exactly one of the two.
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

// Prefix marks a string as an Envi API key. It makes a leaked key recognisable
// in a log or a commit, and lets secret scanners match on it.
const Prefix = "envi_"

// DefaultLifetime is how long a key lasts when the caller doesn't say. A key
// that never expires is still possible, by asking for a lifetime of 0.
const DefaultLifetime = 90 * 24 * time.Hour

// ErrNotFound means the key doesn't exist, or isn't this user's to touch.
var ErrNotFound = errors.New("api key not found")

// Key describes one API key. Secret holds the actual key, and only ever right
// after Create: it is never stored, so it can never be shown again.
type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Permission string     `json:"permission"`
	Secret     string     `json:"secret,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Create makes a new key for a user. lifetime of 0 means it never expires.
func Create(ctx context.Context, db *pgxpool.Pool, userID string, name string, permission string, lifetime time.Duration) (Key, error) {
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

	// Exactly 0 means "never expires". Anything else sets an expiry, including a
	// negative lifetime, which makes an already-expired key rather than an
	// accidentally permanent one.
	var expiresAt *time.Time
	if lifetime != 0 {
		when := time.Now().Add(lifetime)
		expiresAt = &when
	}

	key := Key{Name: name, Permission: permission, Secret: secret, ExpiresAt: expiresAt}
	err = db.QueryRow(ctx,
		`INSERT INTO api_tokens (user_id, token_hash, name, permission, expires_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		userID, auth.HashToken(secret), name, permission, expiresAt,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return Key{}, err
	}
	return key, nil
}

// List returns a user's keys, newest first, without their secrets. Revoked keys
// are left out; expired ones are kept so it's obvious why they stopped working.
func List(ctx context.Context, db *pgxpool.Pool, userID string) ([]Key, error) {
	rows, err := db.Query(ctx,
		`SELECT id, coalesce(name, ''), permission, expires_at, last_used_at, created_at
		 FROM api_tokens
		 WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []Key{}
	for rows.Next() {
		var key Key
		if err = rows.Scan(&key.ID, &key.Name, &key.Permission, &key.ExpiresAt, &key.LastUsedAt, &key.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Revoke stops a key working. Sessions already started from it are ended too,
// otherwise revoking a leaked key would leave whoever took it logged in for the
// rest of the session's life.
func Revoke(ctx context.Context, db *pgxpool.Pool, userID string, keyID string) error {
	result, err := db.Exec(ctx,
		`UPDATE api_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, userID,
	)
	// A malformed id is a typo, not a server fault: report it as "no such key".
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now()
		 WHERE api_key_id = $1 AND revoked_at IS NULL`,
		keyID,
	)
	return err
}

// Authenticate checks a key and reports whose it is and what it may do. It also
// records that the key was used, which is what makes an unused key obvious later.
func Authenticate(ctx context.Context, db *pgxpool.Pool, secret string) (userID string, permission string, keyID string, err error) {
	err = db.QueryRow(ctx,
		`SELECT user_id, permission, id FROM api_tokens
		 WHERE token_hash = $1 AND user_id IS NOT NULL AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > now())`,
		auth.HashToken(secret),
	).Scan(&userID, &permission, &keyID)
	if err != nil {
		return "", "", "", errors.New("invalid api key")
	}
	// Best effort: a failure to record usage must not stop someone logging in.
	_, _ = db.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, keyID)
	return userID, permission, keyID, nil
}

func newSecret() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return Prefix + hex.EncodeToString(randomBytes), nil
}
