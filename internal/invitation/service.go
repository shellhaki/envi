package invitation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/auth"
)

var ErrForbidden = errors.New("forbidden")

type Invitation struct {
	ID, Token, Email, ProjectID, EnvironmentID, Permission string
	ExpiresAt                                              time.Time
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) Create(ctx context.Context, user, project, env, email, permission string, ttl time.Duration) (Invitation, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return Invitation{}, errors.New("valid email required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Invitation{}, errors.New("invalid permission")
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p WHERE p.id=$1 AND (EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=$2 AND m.role IN('owner','admin')) OR EXISTS(SELECT 1 FROM access_grants g WHERE g.project_id=p.id AND g.subject_user_id=$2 AND g.permission='manage')) AND (NULLIF($3::text,'') IS NULL OR EXISTS(SELECT 1 FROM environments e WHERE e.id=NULLIF($3::text,'')::uuid AND e.project_id=p.id)))`, project, user, env).Scan(&allowed)
	if err != nil || !allowed {
		return Invitation{}, ErrForbidden
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return Invitation{}, err
	}
	token := hex.EncodeToString(b)
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	i := Invitation{Token: token, Email: email, ProjectID: project, EnvironmentID: env, Permission: permission, ExpiresAt: time.Now().Add(ttl)}
	err = s.DB.QueryRow(ctx, `INSERT INTO invitations(project_id,environment_id,email,permission,token_hash,invited_by,expires_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7) RETURNING id`, project, env, email, permission, auth.HashToken(token), user, i.ExpiresAt).Scan(&i.ID)
	return i, err
}

func (s Service) Accept(ctx context.Context, user, token string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, project, permission string
	var env *string
	err = tx.QueryRow(ctx, `SELECT i.id,i.project_id,i.environment_id,i.permission FROM invitations i JOIN users u ON lower(u.email)=lower(i.email) WHERE i.token_hash=$1 AND u.id=$2 AND i.status='pending' AND i.expires_at>now() FOR UPDATE OF i`, auth.HashToken(token), user).Scan(&id, &project, &env, &permission)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_grants(subject_user_id,project_id,environment_id,permission) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, user, project, env, permission)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE invitations SET status='accepted' WHERE id=$1`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
