package admin

import (
	"context"
	"errors"
	"strings"
	"time"
)

/*
Who may act on the instance, and what each of them may do.

Two sources, and the order matters:

  - ENVI_ADMINS, the environment variable. Whoever can deploy is an admin, no
    database row required. This is the bootstrap, and it is what stops a write
    to the operators table from being a way to grant yourself the instance.

  - The operators table, which an admin manages from the dashboard.

A row can never override the environment: somebody in ENVI_ADMINS is an admin
whatever the table says, including if a row demotes them to announcer.
*/

// Roles, in descending order of what they may do.
const (
	// RoleAdmin may do everything an operator can, including appointing and
	// removing other operators.
	RoleAdmin = "admin"
	// RoleAnnouncer may write and send announcements. Nothing else — not the
	// instance figures, not expenses, not other operators.
	RoleAnnouncer = "announcer"
)

// Operator is somebody with instance-wide authority.
type Operator struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	// FromEnv marks an operator who is an admin because of ENVI_ADMINS rather
	// than because of a row. The dashboard shows these but cannot remove them:
	// the only way to revoke one is to change the environment and redeploy.
	FromEnv   bool      `json:"from_env"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// MayAnnounce reports whether this role can send announcements.
func MayAnnounce(role string) bool { return role == RoleAdmin || role == RoleAnnouncer }

// MayManage reports whether this role can do everything else — the figures,
// expenses, and appointing operators.
func MayManage(role string) bool { return role == RoleAdmin }

var (
	errOperatorEmail = errors.New("a valid email address is required")
	errOperatorName  = errors.New("name is required")
	errOperatorRole  = errors.New("role must be admin or announcer")
)

/*
RoleFor resolves somebody's role, environment first.

An empty string means they are not an operator at all. envAdmins is the set
from ENVI_ADMINS, already normalised.
*/
func (s Service) RoleFor(ctx context.Context, email string, envAdmins map[string]struct{}) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", nil
	}
	if _, ok := envAdmins[email]; ok {
		return RoleAdmin, nil
	}

	// Survives the table not existing, so the operator screen keeps working
	// between deploying this code and running the migration.
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT to_regclass('public.operators') IS NOT NULL`).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	var role string
	err := s.DB.QueryRow(ctx, `SELECT role FROM operators WHERE email = $1`, email).Scan(&role)
	if err != nil {
		// No row is not an error; it is the ordinary answer for most people.
		return "", nil
	}
	return role, nil
}

// Operators lists everybody, with the environment admins first and marked.
func (s Service) Operators(ctx context.Context, envAdmins map[string]struct{}) ([]Operator, error) {
	out := []Operator{}
	for email := range envAdmins {
		out = append(out, Operator{Email: email, Name: "Deploy admin", Role: RoleAdmin, FromEnv: true})
	}

	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT to_regclass('public.operators') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return out, nil
	}

	rows, err := s.DB.Query(ctx, `
		SELECT id, email, name, role, created_at
		  FROM operators
		 ORDER BY role, created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o Operator
		if err := rows.Scan(&o.ID, &o.Email, &o.Name, &o.Role, &o.CreatedAt); err != nil {
			return nil, err
		}
		// Somebody in both places is shown once, as what the environment says.
		if _, dupe := envAdmins[o.Email]; dupe {
			continue
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// AddOperator appoints somebody. actorID is the user doing the appointing.
func (s Service) AddOperator(ctx context.Context, actorID, email, name, role string) (Operator, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\n") {
		return Operator{}, errOperatorEmail
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Operator{}, errOperatorName
	}
	if role != RoleAdmin && role != RoleAnnouncer {
		return Operator{}, errOperatorRole
	}

	o := Operator{Email: email, Name: name, Role: role}
	var createdBy any
	if actorID != "" {
		createdBy = actorID
	}
	err := s.DB.QueryRow(ctx, `
		INSERT INTO operators (email, name, role, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role
		RETURNING id, created_at
	`, email, name, role, createdBy).Scan(&o.ID, &o.CreatedAt)
	return o, err
}

// RemoveOperator revokes somebody appointed through the table. It cannot touch
// an ENVI_ADMINS entry, which has no row to delete.
func (s Service) RemoveOperator(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM operators WHERE id = $1`, id)
	return err
}
