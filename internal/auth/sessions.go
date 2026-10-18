package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	AccessTokenLifetime  = 15 * time.Minute
	RefreshTokenLifetime = 30 * 24 * time.Hour
)

// StartSession creates a new access and refresh token pair for a user.
func StartSession(db *pgxpool.Pool, userID string) (access, refresh string, err error) {
	return startSession(db, userID, "", "")
}

// StartLimitedSession is a session made from an API key: it can do no more than the key's permission.
func StartLimitedSession(db *pgxpool.Pool, userID, permission, apiKeyID string) (access, refresh string, err error) {
	return startSession(db, userID, permission, apiKeyID)
}

func startSession(db *pgxpool.Pool, userID, permission, apiKeyID string) (access, refresh string, err error) {
	if access, err = newRandomToken(); err != nil {
		return "", "", err
	}
	if refresh, err = newRandomToken(); err != nil {
		return "", "", err
	}
	_, err = db.Exec(context.Background(), `
		INSERT INTO sessions (user_id, refresh_token_hash, access_token_hash, access_expires_at, expires_at, permission, api_key_id)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4), now() + make_interval(secs => $5), $6, $7)`,
		userID, HashToken(refresh), HashToken(access),
		AccessTokenLifetime.Seconds(), RefreshTokenLifetime.Seconds(),
		nullIfEmpty(permission), nullIfEmpty(apiKeyID))
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// RefreshSession swaps a refresh token for a new pair. Each refresh token works once.
func RefreshSession(db *pgxpool.Pool, refreshToken string) (access, refresh string, err error) {
	userID, permission, apiKeyID, err := useRefreshToken(db, refreshToken)
	if err != nil {
		return "", "", err
	}
	return startSession(db, userID, permission, apiKeyID)
}

// EndSession logs out the session behind a refresh token.
func EndSession(db *pgxpool.Pool, refreshToken string) error {
	_, err := db.Exec(context.Background(),
		`UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1`, HashToken(refreshToken))
	return err
}

// UserForAccessToken returns who an access token belongs to and its permission ceiling ("" for none).
func UserForAccessToken(db *pgxpool.Pool, accessToken string) (userID, permission string, err error) {
	var ceiling *string
	err = db.QueryRow(context.Background(), `
		SELECT user_id, permission FROM sessions
		 WHERE access_token_hash = $1 AND revoked_at IS NULL AND access_expires_at > now()`,
		HashToken(accessToken)).Scan(&userID, &ceiling)
	if err != nil {
		return "", "", errors.New("invalid access token")
	}
	return userID, valueOrEmpty(ceiling), nil
}

// useRefreshToken checks a refresh token and revokes it in one transaction, so it can't be used twice.
func useRefreshToken(db *pgxpool.Pool, refreshToken string) (userID, permission, apiKeyID string, err error) {
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", "", "", err
	}
	defer tx.Rollback(ctx)

	var sessionID string
	var ceiling, fromKey *string
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, permission, api_key_id FROM sessions
		 WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		   FOR UPDATE`,
		HashToken(refreshToken)).Scan(&sessionID, &userID, &ceiling, &fromKey)
	if err != nil {
		return "", "", "", errors.New("invalid refresh token")
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, sessionID); err != nil {
		return "", "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", "", err
	}
	return userID, valueOrEmpty(ceiling), valueOrEmpty(fromKey), nil
}

func newRandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken is what is stored instead of a token, so a database leak reveals no usable tokens.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
