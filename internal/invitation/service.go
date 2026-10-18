package invitation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/mailer"
)

var (
	ErrForbidden   = errors.New("forbidden")
	ErrRateLimited = errors.New("too many invitations sent recently; try again later")
)

// Each person may send at most this many invitations per hour.
const invitationsPerHour = 20

type Invitation struct {
	ID, Token, Email, ProjectID, EnvironmentID, Permission string
	ExpiresAt                                              time.Time
}

// Preview is what the accept page shows before the visitor signs in.
type Preview struct {
	Email, ProjectName, EnvironmentName, Permission string
	ExpiresAt                                       time.Time
}

// Collaborator is one row on a project's sharing list: an accepted grant ("active") or an open invitation ("pending").
type Collaborator struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	Permission    string     `json:"permission"`
	EnvironmentID string     `json:"environment_id,omitempty"`
	Status        string     `json:"status"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

type Mailer interface{ Send(mailer.Message) error }

// Billing decides whether an organization may add a person. Nil means no billing.
type Billing interface {
	AllowNewPerson(ctx context.Context, orgID, email string) error
	RecordPerson(ctx context.Context, orgID, userID string) error
}

type Service struct {
	DB      *pgxpool.Pool
	Mailer  Mailer // nil: invitations are created but not emailed
	WebURL  string // the dashboard, for the /invite/<token> link
	Billing Billing
}

// Create invites an email to a project (or one environment of it) and emails the link.
func (s Service) Create(ctx context.Context, userID, projectID, envID, email, permission string, ttl time.Duration) (Invitation, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return Invitation{}, errors.New("valid email required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Invitation{}, errors.New("invalid permission")
	}
	allowed, err := s.canManage(ctx, projectID, userID, envID)
	if err != nil || !allowed {
		return Invitation{}, ErrForbidden
	}
	if err := s.checkRateLimit(ctx, userID); err != nil {
		return Invitation{}, err
	}

	var inviterEmail string
	if err := s.DB.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&inviterEmail); err == nil && strings.EqualFold(inviterEmail, email) {
		return Invitation{}, errors.New("you already have access — you can't invite yourself")
	}

	var hasAccess bool
	err = s.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM users u JOIN projects p ON p.id = $1
		   WHERE lower(u.email) = $2
		     AND (EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = p.org_id AND m.user_id = u.id)
		       OR EXISTS (SELECT 1 FROM access_grants g
		                   WHERE g.subject_user_id = u.id AND g.project_id = p.id
		                     AND (g.environment_id IS NULL OR g.environment_id = NULLIF($3, '')::uuid)))
		)`, projectID, email, envID).Scan(&hasAccess)
	if err != nil {
		return Invitation{}, err
	}
	if hasAccess {
		return Invitation{}, errors.New("this person already has access to this project")
	}

	var alreadyPending bool
	err = s.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM invitations
		   WHERE project_id = $1 AND email = $2 AND status = 'pending' AND expires_at > now()
		     AND environment_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		)`, projectID, email, envID).Scan(&alreadyPending)
	if err != nil {
		return Invitation{}, err
	}
	if alreadyPending {
		return Invitation{}, errors.New("there's already a pending invitation for this address")
	}

	if s.Billing != nil {
		org, err := orgOf(ctx, s.DB, projectID)
		if err != nil {
			return Invitation{}, err
		}
		if err := s.Billing.AllowNewPerson(ctx, org, email); err != nil {
			return Invitation{}, err
		}
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Invitation{}, err
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	inv := Invitation{
		Token: hex.EncodeToString(random), Email: email, ProjectID: projectID,
		EnvironmentID: envID, Permission: permission, ExpiresAt: time.Now().Add(ttl),
	}
	err = s.DB.QueryRow(ctx, `
		INSERT INTO invitations (project_id, environment_id, email, permission, token_hash, invited_by, expires_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7) RETURNING id`,
		projectID, envID, email, permission, auth.HashToken(inv.Token), userID, inv.ExpiresAt).Scan(&inv.ID)
	if err != nil {
		return Invitation{}, err
	}
	s.sendEmail(ctx, inv)
	return inv, nil
}

// Preview returns a pending invitation for the accept page. It needs no sign-in.
func (s Service) Preview(ctx context.Context, token string) (Preview, error) {
	var p Preview
	var envName *string
	err := s.DB.QueryRow(ctx, `
		SELECT i.email, pr.name, e.name, i.permission, i.expires_at
		  FROM invitations i
		  JOIN projects pr ON pr.id = i.project_id
		  LEFT JOIN environments e ON e.id = i.environment_id
		 WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()`,
		auth.HashToken(token)).Scan(&p.Email, &p.ProjectName, &envName, &p.Permission, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, ErrForbidden
	}
	if err != nil {
		return Preview{}, err
	}
	if envName != nil {
		p.EnvironmentName = *envName
	}
	return p, nil
}

// Accept turns an invitation into an access grant. The signed-in user's email must match the invitation.
func (s Service) Accept(ctx context.Context, userID, token string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var invitationID, projectID, permission string
	var envID *string
	err = tx.QueryRow(ctx, `
		SELECT i.id, i.project_id, i.environment_id, i.permission
		  FROM invitations i JOIN users u ON lower(u.email) = lower(i.email)
		 WHERE i.token_hash = $1 AND u.id = $2 AND i.status = 'pending' AND i.expires_at > now()
		   FOR UPDATE OF i`,
		auth.HashToken(token), userID).Scan(&invitationID, &projectID, &envID, &permission)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}

	// Checked again here, or several invitations sent below the limit could all be accepted past it.
	var org string
	if s.Billing != nil {
		if org, err = orgOf(ctx, tx, projectID); err != nil {
			return err
		}
		var email string
		if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
			return err
		}
		if err := s.Billing.AllowNewPerson(ctx, org, email); err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO access_grants (subject_user_id, project_id, environment_id, permission)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, userID, projectID, envID, permission)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE invitations SET status = 'accepted' WHERE id = $1`, invitationID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if s.Billing != nil {
		if err := s.Billing.RecordPerson(ctx, org, userID); err != nil {
			log.Printf("billing: recording %s in %s: %v", userID, org, err) // the hourly sweep catches it anyway
		}
	}
	return nil
}

// ListCollaborators returns everyone with access to a project, then open invitations.
func (s Service) ListCollaborators(ctx context.Context, userID, projectID string) ([]Collaborator, error) {
	allowed, err := s.canManage(ctx, projectID, userID, "")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}

	list := []Collaborator{}
	grants, err := s.DB.Query(ctx, `
		SELECT g.id::text, u.email, g.permission, COALESCE(g.environment_id::text, '')
		  FROM access_grants g JOIN users u ON u.id = g.subject_user_id
		 WHERE g.project_id = $1
		 ORDER BY g.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	for grants.Next() {
		c := Collaborator{Status: "active"}
		if err := grants.Scan(&c.ID, &c.Email, &c.Permission, &c.EnvironmentID); err != nil {
			grants.Close()
			return nil, err
		}
		list = append(list, c)
	}
	grants.Close()
	if err := grants.Err(); err != nil {
		return nil, err
	}

	pending, err := s.DB.Query(ctx, `
		SELECT id::text, email, permission, COALESCE(environment_id::text, ''), expires_at
		  FROM invitations
		 WHERE project_id = $1 AND status = 'pending' AND expires_at > now()
		 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer pending.Close()
	for pending.Next() {
		c := Collaborator{Status: "pending"}
		var expires time.Time
		if err := pending.Scan(&c.ID, &c.Email, &c.Permission, &c.EnvironmentID, &expires); err != nil {
			return nil, err
		}
		c.ExpiresAt = &expires
		list = append(list, c)
	}
	return list, pending.Err()
}

// RevokeInvitation cancels a pending invitation so its link stops working.
func (s Service) RevokeInvitation(ctx context.Context, userID, projectID, invitationID string) error {
	return s.manageExec(ctx, userID, projectID,
		`UPDATE invitations SET status = 'revoked' WHERE id = $1 AND project_id = $2 AND status = 'pending'`, invitationID)
}

// RevokeGrant removes a collaborator's access. Owners have memberships, not grants, so they can't be removed here.
func (s Service) RevokeGrant(ctx context.Context, userID, projectID, grantID string) error {
	return s.manageExec(ctx, userID, projectID,
		`DELETE FROM access_grants WHERE id = $1 AND project_id = $2`, grantID)
}

// manageExec runs a statement on (id, project) if the user may manage the project, ErrForbidden if nothing matched.
func (s Service) manageExec(ctx context.Context, userID, projectID, statement, id string) error {
	allowed, err := s.canManage(ctx, projectID, userID, "")
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	result, err := s.DB.Exec(ctx, statement, id, projectID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrForbidden
	}
	return nil
}

// canManage: an org owner or admin, or anyone with a manage grant. A non-empty envID must belong to the project.
func (s Service) canManage(ctx context.Context, projectID, userID, envID string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM projects p
		   WHERE p.id = $1
		     AND (EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin'))
		       OR EXISTS (SELECT 1 FROM access_grants g WHERE g.project_id = p.id AND g.subject_user_id = $2 AND g.permission = 'manage'))
		     AND (NULLIF($3::text, '') IS NULL
		       OR EXISTS (SELECT 1 FROM environments e WHERE e.id = NULLIF($3::text, '')::uuid AND e.project_id = p.id))
		)`, projectID, userID, envID).Scan(&allowed)
	return allowed, err
}

func (s Service) checkRateLimit(ctx context.Context, userID string) error {
	var sent int
	err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM invitations
		 WHERE invited_by = $1 AND created_at > now() - interval '1 hour'`, userID).Scan(&sent)
	if err != nil {
		return err
	}
	if sent >= invitationsPerHour {
		return ErrRateLimited
	}
	return nil
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// orgOf returns the organization that owns a project, which is who pays for its collaborators.
func orgOf(ctx context.Context, q rowQuerier, projectID string) (string, error) {
	var org string
	err := q.QueryRow(ctx, `SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&org)
	return org, err
}
