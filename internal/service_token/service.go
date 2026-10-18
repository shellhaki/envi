package service_token

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
)

var ErrForbidden = errors.New("forbidden")

type Token struct {
	ID, Value  string
	Permission string
	ExpiresAt  *time.Time
}

type Service struct{ DB *pgxpool.Pool }

// Create makes a token bound to one environment. Only an org owner or admin may. A ttl of 0 never expires.
func (s Service) Create(ctx context.Context, userID, projectID, envID, name, permission string, ttl time.Duration) (Token, error) {
	if name == "" {
		return Token{}, errors.New("token name is required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Token{}, errors.New("invalid permission")
	}
	if !s.canCreate(ctx, userID, projectID, envID) {
		return Token{}, ErrForbidden
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Token{}, err
	}
	value := hex.EncodeToString(random)

	var identityID string
	err := s.DB.QueryRow(ctx, `
		INSERT INTO service_identities (project_id, environment_id, name) VALUES ($1, $2, $3)
		RETURNING id`, projectID, envID, name).Scan(&identityID)
	if err != nil {
		return Token{}, err
	}
	var expiresAt *time.Time
	if ttl != 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}
	_, err = s.DB.Exec(ctx, `
		INSERT INTO api_tokens (service_identity_id, token_hash, permission, expires_at) VALUES ($1, $2, $3, $4)`,
		identityID, auth.HashToken(value), permission, expiresAt)
	return Token{ID: identityID, Value: value, Permission: permission, ExpiresAt: expiresAt}, err
}

// Revoke stops a token working. Only an owner or admin of its project's organization may.
func (s Service) Revoke(ctx context.Context, userID, token string) error {
	result, err := s.DB.Exec(ctx, `
		UPDATE api_tokens t SET revoked_at = now()
		 WHERE t.token_hash = $1
		   AND EXISTS (
		     SELECT 1 FROM service_identities si
		       JOIN projects p ON p.id = si.project_id
		       JOIN memberships m ON m.org_id = p.org_id
		      WHERE si.id = t.service_identity_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')
		   )`, auth.HashToken(token), userID)
	if err == nil && result.RowsAffected() == 0 {
		return ErrForbidden
	}
	return err
}

// Authenticate returns the token's identity, environment and permission.
func (s Service) Authenticate(ctx context.Context, token string) (identityID, envID, permission string, err error) {
	err = s.DB.QueryRow(ctx, `
		SELECT si.id, si.environment_id, t.permission
		  FROM api_tokens t
		  JOIN service_identities si ON si.id = t.service_identity_id
		 WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())`,
		auth.HashToken(token)).Scan(&identityID, &envID, &permission)
	if err != nil {
		return "", "", "", errors.New("invalid service token")
	}
	_, _ = s.DB.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, auth.HashToken(token))
	return identityID, envID, permission, nil
}

// canCreate: the user is an org owner or admin, and the environment belongs to the project.
func (s Service) canCreate(ctx context.Context, userID, projectID, envID string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `
		SELECT EXISTS (
		         SELECT 1 FROM projects p JOIN memberships m ON m.org_id = p.org_id
		          WHERE p.id = $1 AND m.user_id = $2 AND m.role IN ('owner', 'admin')
		       )
		   AND EXISTS (SELECT 1 FROM environments WHERE id = $3 AND project_id = $1)`,
		projectID, userID, envID).Scan(&ok)
	return ok
}
