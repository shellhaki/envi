package service_token

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/auth"
	"time"
)

var ErrForbidden = errors.New("forbidden")

type Token struct {
	ID, Value  string
	Permission string
	ExpiresAt  *time.Time
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) Create(ctx context.Context, user, project, env, name, permission string, ttl time.Duration) (Token, error) {
	if name == "" {
		return Token{}, errors.New("token name is required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Token{}, errors.New("invalid permission")
	}
	if !s.manage(ctx, user, project, env) {
		return Token{}, ErrForbidden
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return Token{}, e
	}
	v := hex.EncodeToString(b)
	var id string
	e := s.DB.QueryRow(ctx, `INSERT INTO service_identities(project_id,environment_id,name)VALUES($1,$2,$3)RETURNING id`, project, env, name).Scan(&id)
	if e != nil {
		return Token{}, e
	}
	var exp *time.Time
	if ttl != 0 {
		x := time.Now().Add(ttl)
		exp = &x
	}
	_, e = s.DB.Exec(ctx, `INSERT INTO api_tokens(service_identity_id,token_hash,permission,expires_at)VALUES($1,$2,$3,$4)`, id, auth.HashToken(v), permission, exp)
	return Token{ID: id, Value: v, Permission: permission, ExpiresAt: exp}, e
}
func (s Service) Revoke(ctx context.Context, user, token string) error {
	tag, e := s.DB.Exec(ctx, `UPDATE api_tokens t SET revoked_at=now() WHERE t.token_hash=$1 AND EXISTS(SELECT 1 FROM service_identities si JOIN projects p ON p.id=si.project_id JOIN memberships m ON m.org_id=p.org_id WHERE si.id=t.service_identity_id AND m.user_id=$2 AND m.role IN('owner','admin'))`, auth.HashToken(token), user)
	if e == nil && tag.RowsAffected() == 0 {
		return ErrForbidden
	}
	return e
}
func (s Service) Authenticate(ctx context.Context, token string) (string, string, string, error) {
	var id, env, permission string
	e := s.DB.QueryRow(ctx, `SELECT si.id,si.environment_id,t.permission FROM api_tokens t JOIN service_identities si ON si.id=t.service_identity_id WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND(t.expires_at IS NULL OR t.expires_at>now())`, auth.HashToken(token)).Scan(&id, &env, &permission)
	if e != nil {
		return "", "", "", errors.New("invalid service token")
	}
	_, _ = s.DB.Exec(ctx, `UPDATE api_tokens SET last_used_at=now() WHERE token_hash=$1`, auth.HashToken(token))
	return id, env, permission, nil
}
func (s Service) manage(ctx context.Context, user, project, env string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE p.id=$1 AND m.user_id=$2 AND m.role IN('owner','admin')) AND EXISTS(SELECT 1 FROM environments WHERE id=$3 AND project_id=$1)`, project, user, env).Scan(&ok)
	return ok
}
