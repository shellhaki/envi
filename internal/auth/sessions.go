package auth

// This file is everything about staying logged in.
//
// When someone logs in they get two random tokens:
//
//   - an ACCESS token, sent with every request. It proves who you are.
//     It only lasts 15 minutes, so a stolen one is useless quickly.
//
//   - a REFRESH token, used for one thing only: getting a new pair of tokens
//     when the access token runs out. Each refresh token works exactly once.
//
// Every refresh starts the RefreshTokenLifetime clock again. So someone who
// keeps using the site stays logged in, and someone who stops is logged out
// RefreshTokenLifetime after their last visit.
//
// The tokens themselves are never stored. The database only keeps a hash of
// each one (see HashToken), so someone who reads the database still can't
// log in as anyone.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// How long each token lasts. To log people out after 3 days of inactivity
// instead of 30, change RefreshTokenLifetime to 3 * 24 * time.Hour.
var (
	AccessTokenLifetime  = 15 * time.Minute
	RefreshTokenLifetime = 30 * 24 * time.Hour
)

// StartSession logs a user in: it makes a new pair of tokens and saves the
// session to the database. Every way of logging in ends here.
func StartSession(db *pgxpool.Pool, userID string) (accessToken string, refreshToken string, err error) {
	// "" means an ordinary login, which can do everything the user can.
	return startSession(db, userID, "", "")
}

// StartLimitedSession logs a user in with a ceiling on what the session may do,
// one of "read", "write" or "manage". API keys use this: a session made from a
// read-only key stays read-only, including after it refreshes.
// apiKeyID ties the session to the key it came from, so revoking that key ends
// this session too.
func StartLimitedSession(db *pgxpool.Pool, userID string, permission string, apiKeyID string) (accessToken string, refreshToken string, err error) {
	return startSession(db, userID, permission, apiKeyID)
}

func startSession(db *pgxpool.Pool, userID string, permission string, apiKeyID string) (accessToken string, refreshToken string, err error) {
	accessToken, err = newRandomToken()
	if err != nil {
		return "", "", err
	}
	refreshToken, err = newRandomToken()
	if err != nil {
		return "", "", err
	}

	// A blank permission is stored as NULL, meaning "no ceiling".
	var ceiling *string
	if permission != "" {
		ceiling = &permission
	}
	var fromKey *string
	if apiKeyID != "" {
		fromKey = &apiKeyID
	}
	_, err = db.Exec(context.Background(),
		`INSERT INTO sessions (user_id, refresh_token_hash, access_token_hash, access_expires_at, expires_at, permission, api_key_id)
		 VALUES ($1, $2, $3, now() + make_interval(secs => $4), now() + make_interval(secs => $5), $6, $7)`,
		userID,
		HashToken(refreshToken),
		HashToken(accessToken),
		AccessTokenLifetime.Seconds(),
		RefreshTokenLifetime.Seconds(),
		ceiling,
		fromKey,
	)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

// RefreshSession swaps a refresh token for a brand-new pair of tokens.
// The old refresh token is used up, so it can never be used again.
// A session keeps its ceiling when it refreshes, so a read-only key can't be
// turned into a full session by waiting fifteen minutes.
func RefreshSession(db *pgxpool.Pool, refreshToken string) (newAccessToken string, newRefreshToken string, err error) {
	userID, permission, apiKeyID, err := useUpRefreshToken(db, refreshToken)
	if err != nil {
		return "", "", err
	}
	return startSession(db, userID, permission, apiKeyID)
}

// EndSession logs out: the session behind this refresh token stops working,
// and so does the access token that came with it.
func EndSession(db *pgxpool.Pool, refreshToken string) error {
	_, err := db.Exec(context.Background(),
		`UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1`,
		HashToken(refreshToken),
	)
	return err
}

// UserForAccessToken answers "who is making this request?". It returns the
// user's ID and the session's ceiling ("" for an ordinary login), or an error if
// the token is unknown, expired, or logged out.
func UserForAccessToken(db *pgxpool.Pool, accessToken string) (userID string, permission string, err error) {
	var ceiling *string
	err = db.QueryRow(context.Background(),
		`SELECT user_id, permission FROM sessions
		 WHERE access_token_hash = $1 AND revoked_at IS NULL AND access_expires_at > now()`,
		HashToken(accessToken),
	).Scan(&userID, &ceiling)
	if err != nil {
		return "", "", errors.New("invalid access token")
	}
	if ceiling != nil {
		permission = *ceiling
	}
	return userID, permission, nil
}

// useUpRefreshToken checks a refresh token and marks it as used, in one step,
// and returns the user it belongs to.
func useUpRefreshToken(db *pgxpool.Pool, refreshToken string) (userID string, permission string, apiKeyID string, err error) {
	ctx := context.Background()

	// A transaction groups the "check" and the "mark as used" together, so
	// nothing can happen in between.
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", "", "", err
	}
	// If we return early for any reason, undo whatever the transaction did.
	// (After Commit below succeeds, this does nothing.)
	defer tx.Rollback(ctx)

	// Find a session with this refresh token that hasn't been used and hasn't
	// expired. FOR UPDATE locks the row: if two browser tabs refresh at the
	// same moment, the second one waits, then finds the token already used.
	var sessionID string
	var ceiling, fromKey *string
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, permission, api_key_id FROM sessions
		 WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		 FOR UPDATE`,
		HashToken(refreshToken),
	).Scan(&sessionID, &userID, &ceiling, &fromKey)
	if err != nil {
		return "", "", "", errors.New("invalid refresh token")
	}

	// Mark the session as used, which also kills its access token.
	_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, sessionID)
	if err != nil {
		return "", "", "", err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return "", "", "", err
	}
	if ceiling != nil {
		permission = *ceiling
	}
	if fromKey != nil {
		apiKeyID = *fromKey
	}
	return userID, permission, apiKeyID, nil
}

// newRandomToken makes 32 random bytes, written as 64 hex characters.
func newRandomToken() (string, error) {
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

// HashToken turns a token into the fingerprint we store instead of the token.
// Service tokens and invitations use this too.
func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
