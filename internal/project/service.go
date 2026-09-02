package project

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("forbidden")

type Project struct{ ID, OrgID, Name string }
type Environment struct {
	ID, ProjectID, Name string
	Production          bool
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) Create(ctx context.Context, userID, orgID, name string) (Project, error) {
	if !s.member(ctx, userID, orgID) {
		return Project{}, ErrForbidden
	}
	var p Project
	err := s.DB.QueryRow(ctx, `INSERT INTO projects(org_id,name) VALUES($1,$2) RETURNING id,org_id,name`, orgID, name).Scan(&p.ID, &p.OrgID, &p.Name)
	return p, err
}
func (s Service) List(ctx context.Context, userID string) ([]Project, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT p.id,p.org_id,p.name FROM projects p LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=$1 LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=$1 WHERE m.id IS NOT NULL OR g.id IS NOT NULL ORDER BY p.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.OrgID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s Service) Delete(ctx context.Context, userID, id string) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM projects p USING memberships m WHERE p.id=$1 AND m.org_id=p.org_id AND m.user_id=$2 AND m.role IN('owner','admin')`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrForbidden
	}
	return err
}
func (s Service) CreateEnvironment(ctx context.Context, userID, projectID, name string, production bool) (Environment, error) {
	if !s.projectManager(ctx, userID, projectID) {
		return Environment{}, ErrForbidden
	}
	var e Environment
	err := s.DB.QueryRow(ctx, `INSERT INTO environments(project_id,name,is_production) VALUES($1,$2,$3) RETURNING id,project_id,name,is_production`, projectID, name, production).Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production)
	return e, err
}
func (s Service) ListEnvironments(ctx context.Context, userID, projectID string) ([]Environment, error) {
	if !s.projectViewer(ctx, userID, projectID) {
		return nil, ErrForbidden
	}
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT e.id,e.project_id,e.name,e.is_production FROM environments e JOIN projects p ON p.id=e.project_id LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=$2 LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=$2 AND (g.environment_id=e.id OR (g.environment_id IS NULL AND NOT e.is_production)) WHERE p.id=$1 AND (m.id IS NOT NULL OR g.id IS NOT NULL) ORDER BY e.name`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Environment{}
	for rows.Next() {
		var e Environment
		if err = rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s Service) UpdateEnvironment(ctx context.Context, userID, id, name string, production bool) (Environment, error) {
	var e Environment
	err := s.DB.QueryRow(ctx, `UPDATE environments e SET name=$1,is_production=$2 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE e.id=$3 AND p.id=e.project_id AND m.user_id=$4 RETURNING e.id,e.project_id,e.name,e.is_production`, name, production, id, userID).Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrForbidden
	}
	return e, err
}
func (s Service) DeleteEnvironment(ctx context.Context, userID, id string) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM environments e USING projects p,memberships m WHERE e.id=$1 AND p.id=e.project_id AND m.org_id=p.org_id AND m.user_id=$2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrForbidden
	}
	return err
}
func (s Service) member(ctx context.Context, u, o string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND org_id=$2 AND role IN('owner','admin'))`, u, o).Scan(&ok)
	return ok
}
func (s Service) projectMember(ctx context.Context, u, p string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE p.id=$1 AND m.user_id=$2)`, p, u).Scan(&ok)
	return ok
}
func (s Service) projectManager(ctx context.Context, u, p string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=$2 AND m.role IN('owner','admin') LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=$2 AND g.permission='manage' WHERE p.id=$1 AND (m.id IS NOT NULL OR g.id IS NOT NULL))`, p, u).Scan(&ok)
	return ok
}
func (s Service) projectViewer(ctx context.Context, u, p string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=$2 LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=$2 WHERE p.id=$1 AND (m.id IS NOT NULL OR g.id IS NOT NULL))`, p, u).Scan(&ok)
	return ok
}
