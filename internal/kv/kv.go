// Package kv is the project-wide key-value store the SDK reads and writes.
//
// It sits beside secrets rather than inside them. A secret belongs to one
// environment, keeps every version it has ever had, and is reached through
// per-environment grants. A kv entry belongs to the whole project, keeps only
// its current value, and is the thing an application sets while it runs.
//
// Values are encrypted with the same cipher as secrets, so a stolen database
// still gives up nothing readable.
package kv

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	crypt "shellhaki/envi/internal/crypto"
)

// ErrForbidden means the caller may not touch this project.
var ErrForbidden = errors.New("forbidden")

// ErrNotFound means there is no entry under that key.
var ErrNotFound = errors.New("key not found")

// MaxValueBytes caps a single value. Generous for configuration, small enough
// that the store cannot quietly become a file host.
const MaxValueBytes = 64 << 10

type Store struct {
	DB     *pgxpool.Pool
	Cipher *crypt.Cipher
}

// All returns every entry in a project, decrypted.
func (s Store) All(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT key_name, ciphertext FROM kv_entries WHERE project_id = $1 ORDER BY key_name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key string
		var sealed []byte
		if err = rows.Scan(&key, &sealed); err != nil {
			return nil, err
		}
		plain, err := s.Cipher.Open(sealed)
		if err != nil {
			return nil, err
		}
		values[key] = string(plain)
	}
	return values, rows.Err()
}

// Get returns one value.
func (s Store) Get(ctx context.Context, projectID, key string) (string, error) {
	var sealed []byte
	err := s.DB.QueryRow(ctx,
		`SELECT ciphertext FROM kv_entries WHERE project_id = $1 AND key_name = $2`, projectID, key).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	plain, err := s.Cipher.Open(sealed)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Set writes a value, replacing whatever was there. Unlike a secret, no history
// is kept: this is current state, not a record of changes.
func (s Store) Set(ctx context.Context, projectID, key, value string) error {
	key = strings.TrimSpace(key)
	if err := ValidKey(key); err != nil {
		return err
	}
	if len(value) > MaxValueBytes {
		return errors.New("value is too large; the limit is 64KB")
	}
	sealed, err := s.Cipher.Seal([]byte(value))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx,
		`INSERT INTO kv_entries (project_id, key_name, ciphertext) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, key_name) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = now()`,
		projectID, key, sealed)
	return err
}

// Delete removes an entry. Deleting something that was never there is not an
// error worth reporting to a caller trying to make it gone.
func (s Store) Delete(ctx context.Context, projectID, key string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM kv_entries WHERE project_id = $1 AND key_name = $2`, projectID, key)
	return err
}

// ValidKey rejects names that would be confusing or unusable. Anything printable
// is allowed otherwise: this is the caller's namespace, not ours.
func ValidKey(key string) error {
	switch {
	case key == "":
		return errors.New("a key is required")
	case len(key) > 255:
		return errors.New("key is too long; the limit is 255 characters")
	case strings.ContainsAny(key, "\x00\n\r"):
		return errors.New("a key cannot contain newlines or null bytes")
	default:
		return nil
	}
}
