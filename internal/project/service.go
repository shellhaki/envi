package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// DefaultEnvironmentName is the environment every new project starts with.
const DefaultEnvironmentName = "default"

// Create makes a project and its default environment in one transaction.
func (s Service) Create(ctx context.Context, userID, orgID, name string) (Project, error) {
	if !s.isOrgAdmin(ctx, userID, orgID) {
		return Project{}, ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)

	var p Project
	err = tx.QueryRow(ctx, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id, org_id, name`,
		orgID, name).Scan(&p.ID, &p.OrgID, &p.Name)
	if err != nil {
		return Project{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO environments (project_id, name, is_production) VALUES ($1, $2, false)`,
		p.ID, DefaultEnvironmentName)
	if err != nil {
		return Project{}, err
	}
	return p, tx.Commit(ctx)
}

// List returns the projects the user is a member of or has been invited to.
func (s Service) List(ctx context.Context, userID string) ([]Project, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT p.id, p.org_id, p.name
		  FROM projects p
		  LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $1
		  LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $1
		 WHERE m.id IS NOT NULL OR g.id IS NOT NULL
		 ORDER BY p.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// Delete removes the project and, by cascade, everything in it. Only an org owner or admin may.
func (s Service) Delete(ctx context.Context, userID, projectID string) error {
	result, err := s.DB.Exec(ctx, `
		DELETE FROM projects p USING memberships m
		 WHERE p.id = $1 AND m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')`,
		projectID, userID)
	if err == nil && result.RowsAffected() == 0 {
		return ErrForbidden
	}
	return err
}

func (s Service) CreateEnvironment(ctx context.Context, userID, projectID, name string, production bool) (Environment, error) {
	if !s.canManage(ctx, userID, projectID) {
		return Environment{}, ErrForbidden
	}
	var e Environment
	err := s.DB.QueryRow(ctx, `
		INSERT INTO environments (project_id, name, is_production) VALUES ($1, $2, $3)
		RETURNING id, project_id, name, is_production`,
		projectID, name, production).Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production)
	return e, err
}

// ListEnvironments returns the environments the user can see in a project.
func (s Service) ListEnvironments(ctx context.Context, userID, projectID string) ([]Environment, error) {
	if !s.CanView(ctx, userID, projectID) {
		return nil, ErrForbidden
	}
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT e.id, e.project_id, e.name, e.is_production, e.created_at
		  FROM environments e
		  JOIN projects p ON p.id = e.project_id
		  LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
		  LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2
		       AND (g.environment_id = e.id OR (g.environment_id IS NULL AND NOT e.is_production))
		 WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
		 ORDER BY e.created_at, e.name`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	environments := []Environment{}
	for rows.Next() {
		var e Environment
		var createdAt time.Time
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production, &createdAt); err != nil {
			return nil, err
		}
		environments = append(environments, e)
	}
	return environments, rows.Err()
}

// UpdateEnvironment renames an environment or changes its production flag. Only an org owner or admin may.
func (s Service) UpdateEnvironment(ctx context.Context, userID, envID, name string, production bool) (Environment, error) {
	var e Environment
	err := s.DB.QueryRow(ctx, `
		UPDATE environments e SET name = $1, is_production = $2
		  FROM projects p JOIN memberships m ON m.org_id = p.org_id
		 WHERE e.id = $3 AND p.id = e.project_id AND m.user_id = $4 AND m.role IN ('owner', 'admin')
		RETURNING e.id, e.project_id, e.name, e.is_production`,
		name, production, envID, userID).Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrForbidden
	}
	return e, err
}

// DeleteEnvironment removes an environment and its secrets. Only an org owner or admin may.
func (s Service) DeleteEnvironment(ctx context.Context, userID, envID string) error {
	result, err := s.DB.Exec(ctx, `
		DELETE FROM environments e USING projects p, memberships m
		 WHERE e.id = $1 AND p.id = e.project_id AND m.org_id = p.org_id
		   AND m.user_id = $2 AND m.role IN ('owner', 'admin')`,
		envID, userID)
	if err == nil && result.RowsAffected() == 0 {
		return ErrForbidden
	}
	return err
}

// FindByName finds one of the user's projects by name. The error lists their projects, to spot a typo.
func (s Service) FindByName(ctx context.Context, userID, name string) (Project, error) {
	projects, err := s.List(ctx, userID)
	if err != nil {
		return Project{}, err
	}
	var names []string
	for _, p := range projects {
		if p.Name == name {
			return p, nil
		}
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return Project{}, fmt.Errorf("no project named %q, and this account has no projects", name)
	}
	return Project{}, fmt.Errorf("no project named %q; this account can see: %s", name, strings.Join(names, ", "))
}

// FindEnvironmentByName finds an environment in a project. The name may be empty if there is only one.
func (s Service) FindEnvironmentByName(ctx context.Context, userID, projectID, name string) (Environment, error) {
	environments, err := s.ListEnvironments(ctx, userID, projectID)
	if err != nil {
		return Environment{}, err
	}
	if len(environments) == 0 {
		return Environment{}, errors.New("this project has no environments yet")
	}
	var names []string
	for _, e := range environments {
		if e.Name == name {
			return e, nil
		}
		names = append(names, e.Name)
	}
	if name == "" {
		if len(environments) == 1 {
			return environments[0], nil
		}
		return Environment{}, fmt.Errorf("this project has several environments, so one must be named: %s", strings.Join(names, ", "))
	}
	return Environment{}, fmt.Errorf("no environment named %q in this project; it has: %s", name, strings.Join(names, ", "))
}

// DescribeEnvironment returns an environment's name and its project's name, with no permission check.
func (s Service) DescribeEnvironment(ctx context.Context, envID string) (envName, projectName string, err error) {
	err = s.DB.QueryRow(ctx, `
		SELECT e.name, p.name FROM environments e JOIN projects p ON p.id = e.project_id
		 WHERE e.id = $1`, envID).Scan(&envName, &projectName)
	return envName, projectName, err
}

// ProjectForEnvironment returns the ID and name of the project an environment belongs to.
func (s Service) ProjectForEnvironment(ctx context.Context, envID string) (id, name string, err error) {
	err = s.DB.QueryRow(ctx, `
		SELECT p.id, p.name FROM projects p JOIN environments e ON e.project_id = p.id
		 WHERE e.id = $1`, envID).Scan(&id, &name)
	return id, name, err
}

// CanView: a member of the project's organization, or anyone with a grant on it.
func (s Service) CanView(ctx context.Context, userID, projectID string) bool {
	return s.exists(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM projects p
		    LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
		    LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2
		   WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
		)`, projectID, userID)
}

// CanWrite (project-wide data, like the key-value store): a member, or anyone with a write or manage grant.
func (s Service) CanWrite(ctx context.Context, userID, projectID string) bool {
	return s.exists(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM projects p
		    LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
		    LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2 AND g.permission IN ('write', 'manage')
		   WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
		)`, projectID, userID)
}

// canManage: an org owner or admin, or anyone with a manage grant on the project.
func (s Service) canManage(ctx context.Context, userID, projectID string) bool {
	return s.exists(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM projects p
		    LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')
		    LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2 AND g.permission = 'manage'
		   WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
		)`, projectID, userID)
}

func (s Service) isOrgAdmin(ctx context.Context, userID, orgID string) bool {
	return s.exists(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM memberships WHERE user_id = $1 AND org_id = $2 AND role IN ('owner', 'admin')
		)`, userID, orgID)
}

// exists runs a SELECT EXISTS query; any error counts as false.
func (s Service) exists(ctx context.Context, query string, args ...any) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, query, args...).Scan(&ok)
	return ok
}
