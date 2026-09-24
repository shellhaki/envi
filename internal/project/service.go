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

// DefaultEnvironmentName is the environment every new project starts with. A
// project with none is unusable: envi init has nothing to link a directory to.
const DefaultEnvironmentName = "default"

// Create makes a project and its first environment together, in one
// transaction. Both or neither: a project with no environment cannot be
// initialized from the CLI, and the dashboard used to paper over that by
// provisioning one on first view, which left CLI-created projects broken.
func (s Service) Create(ctx context.Context, userID, orgID, name string) (Project, error) {
	if !s.member(ctx, userID, orgID) {
		return Project{}, ErrForbidden
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)

	var p Project
	if err = tx.QueryRow(ctx, `INSERT INTO projects(org_id,name) VALUES($1,$2) RETURNING id,org_id,name`, orgID, name).Scan(&p.ID, &p.OrgID, &p.Name); err != nil {
		return Project{}, err
	}
	// Not production, so org members reach it without needing a grant.
	if _, err = tx.Exec(ctx, `INSERT INTO environments(project_id,name,is_production) VALUES($1,$2,false)`, p.ID, DefaultEnvironmentName); err != nil {
		return Project{}, err
	}
	return p, tx.Commit(ctx)
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
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT e.id,e.project_id,e.name,e.is_production,e.created_at FROM environments e JOIN projects p ON p.id=e.project_id LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=$2 LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=$2 AND (g.environment_id=e.id OR (g.environment_id IS NULL AND NOT e.is_production)) WHERE p.id=$1 AND (m.id IS NOT NULL OR g.id IS NOT NULL) ORDER BY e.created_at,e.name`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Environment{}
	for rows.Next() {
		var e Environment
		var createdAt time.Time
		if err = rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s Service) UpdateEnvironment(ctx context.Context, userID, id, name string, production bool) (Environment, error) {
	var e Environment
	err := s.DB.QueryRow(ctx, `UPDATE environments e SET name=$1,is_production=$2 FROM projects p JOIN memberships m ON m.org_id=p.org_id WHERE e.id=$3 AND p.id=e.project_id AND m.user_id=$4 AND m.role IN('owner','admin') RETURNING e.id,e.project_id,e.name,e.is_production`, name, production, id, userID).Scan(&e.ID, &e.ProjectID, &e.Name, &e.Production)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrForbidden
	}
	return e, err
}
func (s Service) DeleteEnvironment(ctx context.Context, userID, id string) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM environments e USING projects p,memberships m WHERE e.id=$1 AND p.id=e.project_id AND m.org_id=p.org_id AND m.user_id=$2 AND m.role IN('owner','admin')`, id, userID)
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

// FindByName resolves a project by name among the ones a user can see. It goes
// through List so the "can see" rule is defined in exactly one place.
//
// On a miss the error names the projects they do have: the likely cause is a
// typo or the wrong account, and the list is the quickest way to spot which.
func (s Service) FindByName(ctx context.Context, userID, name string) (Project, error) {
	projects, err := s.List(ctx, userID)
	if err != nil {
		return Project{}, err
	}
	available := make([]string, 0, len(projects))
	for _, p := range projects {
		if p.Name == name {
			return p, nil
		}
		available = append(available, p.Name)
	}
	if len(available) == 0 {
		return Project{}, fmt.Errorf("no project named %q, and this account has no projects", name)
	}
	return Project{}, fmt.Errorf("no project named %q; this account can see: %s", name, strings.Join(available, ", "))
}

// FindEnvironmentByName resolves an environment within a project by name. An
// empty name is allowed when the project has exactly one environment, so a
// single-environment project needs no configuration.
func (s Service) FindEnvironmentByName(ctx context.Context, userID, projectID, name string) (Environment, error) {
	environments, err := s.ListEnvironments(ctx, userID, projectID)
	if err != nil {
		return Environment{}, err
	}
	if len(environments) == 0 {
		return Environment{}, errors.New("this project has no environments yet")
	}
	if name == "" {
		if len(environments) == 1 {
			return environments[0], nil
		}
		return Environment{}, fmt.Errorf("this project has several environments, so one must be named: %s", strings.Join(environmentNames(environments), ", "))
	}
	for _, e := range environments {
		if e.Name == name {
			return e, nil
		}
	}
	return Environment{}, fmt.Errorf("no environment named %q in this project; it has: %s", name, strings.Join(environmentNames(environments), ", "))
}

// DescribeEnvironment names an environment and its project, with no permission
// check: it answers "what is this id?" for a caller that already holds a
// credential bound to that exact environment.
func (s Service) DescribeEnvironment(ctx context.Context, environmentID string) (environmentName string, projectName string, err error) {
	err = s.DB.QueryRow(ctx,
		`SELECT e.name, p.name FROM environments e JOIN projects p ON p.id = e.project_id WHERE e.id = $1`,
		environmentID,
	).Scan(&environmentName, &projectName)
	return environmentName, projectName, err
}

func environmentNames(environments []Environment) []string {
	names := make([]string, 0, len(environments))
	for _, e := range environments {
		names = append(names, e.Name)
	}
	return names
}

// CanView reports whether a user may read a project at all: a member of its
// organization, or someone holding any grant on it.
func (s Service) CanView(ctx context.Context, userID, projectID string) bool {
	return s.projectViewer(ctx, userID, projectID)
}

// CanWrite reports whether a user may change project-wide data, such as the
// key-value store. Org membership is enough — the same bar as reading and
// writing a non-production environment — plus anyone with a write or manage
// grant on the project.
//
// This deliberately does not use access.Allow: that answers a question about
// one environment, and these values belong to the whole project.
func (s Service) CanWrite(ctx context.Context, userID, projectID string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM projects p
		   LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
		   LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2 AND g.permission IN ('write','manage')
		   WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL))`,
		projectID, userID).Scan(&ok)
	return ok
}

// ProjectForEnvironment names the project an environment belongs to. A service
// token is bound to an environment but the key-value store is project-wide, so
// this is how a token finds the project it may act on.
func (s Service) ProjectForEnvironment(ctx context.Context, environmentID string) (id string, name string, err error) {
	err = s.DB.QueryRow(ctx,
		`SELECT p.id, p.name FROM projects p JOIN environments e ON e.project_id = p.id WHERE e.id = $1`,
		environmentID).Scan(&id, &name)
	return id, name, err
}
