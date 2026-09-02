package access

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("forbidden")

type Service struct{ DB *pgxpool.Pool }

func (s Service) Allow(ctx context.Context, user, env, need string) error {
	var prod, ok bool
	e := s.DB.QueryRow(ctx, `SELECT e.is_production,EXISTS(SELECT 1 FROM access_grants g WHERE g.subject_user_id=$1 AND g.project_id=e.project_id AND (g.environment_id=e.id OR(g.environment_id IS NULL AND NOT e.is_production)) AND CASE $3 WHEN'read'THEN g.permission IN('read','write','manage') WHEN'write'THEN g.permission IN('write','manage') ELSE g.permission='manage' END) OR (NOT e.is_production AND EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE p.id=e.project_id AND m.user_id=$1)) OR EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE p.id=e.project_id AND m.user_id=$1 AND m.role IN('owner','admin')) FROM environments e WHERE e.id=$2`, user, env, need).Scan(&prod, &ok)
	_ = prod
	if e != nil || !ok {
		return ErrForbidden
	}
	return nil
}
