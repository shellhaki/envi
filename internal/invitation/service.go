package invitation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/mailer"
)

var ErrForbidden = errors.New("forbidden")

// A compromised or careless owner account could otherwise mass-invite
// addresses and burn the sending domain's reputation — mirrors the OTP
// package's own default request limit.
const inviteRateLimit = 20
const inviteRateWindow = time.Hour

var ErrRateLimited = errors.New("too many invitations sent recently; try again later")

type Invitation struct {
	ID, Token, Email, ProjectID, EnvironmentID, Permission string
	ExpiresAt                                              time.Time
}

// Preview is the information a not-yet-authenticated visitor sees when they
// open an invitation link, before proving they own the invited address.
type Preview struct {
	Email, ProjectName, EnvironmentName, Permission string
	ExpiresAt                                       time.Time
}

// Mailer delivers the invitation email. Left nil, Create still creates the
// invitation and returns its token — the caller (e.g. the CLI) is then
// responsible for sharing it.
type Mailer interface{ Send(mailer.Message) error }

type Service struct {
	DB     *pgxpool.Pool
	Mailer Mailer
	// WebURL is the web app's origin, used to build the /invite/<token> link
	// that goes out in the email.
	WebURL string
}

func (s Service) Create(ctx context.Context, user, project, env, email, permission string, ttl time.Duration) (Invitation, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return Invitation{}, errors.New("valid email required")
	}
	if permission != "read" && permission != "write" && permission != "manage" {
		return Invitation{}, errors.New("invalid permission")
	}
	allowed, err := s.canManage(ctx, project, user, env)
	if err != nil || !allowed {
		return Invitation{}, ErrForbidden
	}
	if err := s.checkRateLimit(ctx, user); err != nil {
		return Invitation{}, err
	}
	var inviterEmail string
	if err := s.DB.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, user).Scan(&inviterEmail); err == nil && strings.EqualFold(inviterEmail, email) {
		return Invitation{}, errors.New("you already have access — you can't invite yourself")
	}
	// An invited address that already has access (a member of the project's
	// org, or an existing grant covering this environment) would just get a
	// pointless email and a redundant access_grants row. Checked against the
	// account, not the raw address, since that's what actually holds access.
	var hasAccess bool
	err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN projects p ON p.id=$1 WHERE lower(u.email)=$2 AND (EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=u.id) OR EXISTS(SELECT 1 FROM access_grants g WHERE g.subject_user_id=u.id AND g.project_id=p.id AND (g.environment_id IS NULL OR g.environment_id=NULLIF($3,'')::uuid))))`, project, email, env).Scan(&hasAccess)
	if err != nil {
		return Invitation{}, err
	}
	if hasAccess {
		return Invitation{}, errors.New("this person already has access to this project")
	}
	// Without this, the same address can be invited over and over — confusing
	// for the recipient, and exactly the repeated-send pattern mail providers
	// flag as spammy.
	var alreadyPending bool
	err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM invitations WHERE project_id=$1 AND email=$2 AND status='pending' AND expires_at>now() AND environment_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid)`, project, email, env).Scan(&alreadyPending)
	if err != nil {
		return Invitation{}, err
	}
	if alreadyPending {
		return Invitation{}, errors.New("there's already a pending invitation for this address")
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
	if err != nil {
		return Invitation{}, err
	}
	s.notify(ctx, i)
	return i, nil
}

// canManage reports whether user can invite and manage collaborators on
// project — an org owner/admin, or anyone already holding a 'manage' grant.
// env narrows the check to a specific environment; empty means the project
// as a whole.
func (s Service) canManage(ctx context.Context, project, user, env string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p WHERE p.id=$1 AND (EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=$2 AND m.role IN('owner','admin')) OR EXISTS(SELECT 1 FROM access_grants g WHERE g.project_id=p.id AND g.subject_user_id=$2 AND g.permission='manage')) AND (NULLIF($3::text,'') IS NULL OR EXISTS(SELECT 1 FROM environments e WHERE e.id=NULLIF($3::text,'')::uuid AND e.project_id=p.id)))`, project, user, env).Scan(&allowed)
	return allowed, err
}

// checkRateLimit caps how many invitations one inviter can send per window.
// Without it, a compromised account (or a careless script) could mass-invite
// addresses and tank the sending domain's reputation for everyone.
func (s Service) checkRateLimit(ctx context.Context, user string) error {
	var n int
	// The window is a compile-time constant, not user input, so building the
	// interval into the query text (rather than binding a Go Duration, which
	// Postgres's interval parser doesn't understand the "1h0m0s" form of) is
	// safe here.
	q := fmt.Sprintf(`SELECT count(*) FROM invitations WHERE invited_by=$1 AND created_at > now() - interval '%d seconds'`, int64(inviteRateWindow.Seconds()))
	if err := s.DB.QueryRow(ctx, q, user).Scan(&n); err != nil {
		return err
	}
	if n >= inviteRateLimit {
		return ErrRateLimited
	}
	return nil
}

// notify emails the invitation link, if a mailer is configured. Delivery
// failure does not fail the invitation: it already exists and its token is
// returned to the caller (the CLI prints it, the UI can offer it as a
// fallback), so a flaky mail provider cannot block sharing access.
func (s Service) notify(ctx context.Context, i Invitation) {
	if s.Mailer == nil {
		return
	}
	var projectName string
	_ = s.DB.QueryRow(ctx, `SELECT name FROM projects WHERE id=$1`, i.ProjectID).Scan(&projectName)
	if projectName == "" {
		projectName = "a project"
	}
	link := strings.TrimRight(s.WebURL, "/") + "/invite/" + i.Token
	// A local WebURL with a real mail provider configured sends a real person a
	// link that only resolves on the sender's own machine. It is a legitimate
	// setup for local testing, so it is not fatal, but it must not be silent.
	if strings.Contains(s.WebURL, "localhost") || strings.Contains(s.WebURL, "127.0.0.1") {
		log.Printf("WARNING: emailing %s an invitation link on %s — it will not resolve for them. Set ENVI_WEB_URL to the public dashboard URL.", i.Email, s.WebURL)
	}
	subject := fmt.Sprintf("You're invited to %s on Envi", projectName)
	expiry := i.ExpiresAt.Format("Jan 2, 2006 at 3:04 PM MST")
	text := fmt.Sprintf(
		"You've been invited to collaborate on %s on Envi, with %s access.\n\n"+
			"Accept your invitation: %s\n\n"+
			"If you don't have an Envi account yet, this link will let you create one with %s access already waiting.\n\n"+
			"This invitation expires on %s.",
		projectName, i.Permission, link, i.Permission, expiry,
	)
	html := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
<h1 style="margin:0 0 16px;font-size:20px;">You&rsquo;re invited to %s</h1>
<p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">You&rsquo;ve been invited to collaborate on <strong>%s</strong> with <strong>%s</strong> access.</p>
<a href="%s" style="display:inline-block;background:#0c111d;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:12px 22px;border-radius:8px;">Accept invitation</a>
<p style="margin:24px 0 0;color:#667085;font-size:13px;line-height:1.6;">Don&rsquo;t have an Envi account yet? This link creates one for you, with access already waiting.</p>
<p style="margin:16px 0 0;color:#98a2b3;font-size:12px;line-height:1.6;">This invitation expires on %s. If the button doesn&rsquo;t work, copy this link:<br><span style="word-break:break-all;">%s</span></p>
</div></body></html>`,
		projectName, projectName, i.Permission, link, expiry, link,
	)
	if err := s.Mailer.Send(mailer.Message{To: i.Email, Subject: subject, Text: text, HTML: html}); err != nil {
		log.Printf("invitation email delivery failed: %v", err)
	}
}

// Preview returns the public, pre-authentication view of a pending
// invitation — enough for the accept page to show who invited whom to what,
// without requiring the visitor to be signed in yet.
func (s Service) Preview(ctx context.Context, token string) (Preview, error) {
	var p Preview
	var envName *string
	err := s.DB.QueryRow(ctx, `SELECT i.email,pr.name,e.name,i.permission,i.expires_at FROM invitations i JOIN projects pr ON pr.id=i.project_id LEFT JOIN environments e ON e.id=i.environment_id WHERE i.token_hash=$1 AND i.status='pending' AND i.expires_at>now()`, auth.HashToken(token)).Scan(&p.Email, &p.ProjectName, &envName, &p.Permission, &p.ExpiresAt)
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

// Collaborator is one row in a project's sharing list — either an accepted
// grant (Status "active") or a still-open invitation (Status "pending",
// ExpiresAt set). Both shapes reuse the same struct since the dashboard
// renders them as one list.
type Collaborator struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	Permission    string     `json:"permission"`
	EnvironmentID string     `json:"environment_id,omitempty"`
	Status        string     `json:"status"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// ListCollaborators returns everyone with access to project, or an open
// invitation to get it — the thing the dashboard's Sharing page had no way
// to answer before, short of asking someone to check the database.
func (s Service) ListCollaborators(ctx context.Context, user, project string) ([]Collaborator, error) {
	allowed, err := s.canManage(ctx, project, user, "")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	out := []Collaborator{}
	grants, err := s.DB.Query(ctx, `SELECT g.id::text,u.email,g.permission,COALESCE(g.environment_id::text,'') FROM access_grants g JOIN users u ON u.id=g.subject_user_id WHERE g.project_id=$1 ORDER BY g.created_at DESC`, project)
	if err != nil {
		return nil, err
	}
	for grants.Next() {
		var c Collaborator
		if err := grants.Scan(&c.ID, &c.Email, &c.Permission, &c.EnvironmentID); err != nil {
			grants.Close()
			return nil, err
		}
		c.Status = "active"
		out = append(out, c)
	}
	grants.Close()
	if err := grants.Err(); err != nil {
		return nil, err
	}
	pending, err := s.DB.Query(ctx, `SELECT id::text,email,permission,COALESCE(environment_id::text,''),expires_at FROM invitations WHERE project_id=$1 AND status='pending' AND expires_at>now() ORDER BY created_at DESC`, project)
	if err != nil {
		return nil, err
	}
	defer pending.Close()
	for pending.Next() {
		var c Collaborator
		var exp time.Time
		if err := pending.Scan(&c.ID, &c.Email, &c.Permission, &c.EnvironmentID, &exp); err != nil {
			return nil, err
		}
		c.Status = "pending"
		c.ExpiresAt = &exp
		out = append(out, c)
	}
	if err := pending.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeInvitation cancels a still-pending invitation so its link stops
// working — for inviting the wrong address, or one sent by mistake.
func (s Service) RevokeInvitation(ctx context.Context, user, project, invitationID string) error {
	allowed, err := s.canManage(ctx, project, user, "")
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	ct, err := s.DB.Exec(ctx, `UPDATE invitations SET status='revoked' WHERE id=$1 AND project_id=$2 AND status='pending'`, invitationID, project)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrForbidden
	}
	return nil
}

// RevokeGrant removes an accepted collaborator's access outright. This only
// ever touches access_grants rows — an owner's access comes from
// memberships, not a grant, so there is no path here to lock out a project's
// owner.
func (s Service) RevokeGrant(ctx context.Context, user, project, grantID string) error {
	allowed, err := s.canManage(ctx, project, user, "")
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	ct, err := s.DB.Exec(ctx, `DELETE FROM access_grants WHERE id=$1 AND project_id=$2`, grantID, project)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrForbidden
	}
	return nil
}
