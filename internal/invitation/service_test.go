package invitation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/workspace"
)

func TestCreateValidation(t *testing.T) {
	s := Service{}
	if _, err := s.Create(context.Background(), "", "", "", "bad", "read", 0); err == nil {
		t.Fatal("invalid email accepted")
	}
	if _, err := s.Create(context.Background(), "", "", "", "a@b.com", "owner", 0); err == nil {
		t.Fatal("invalid permission accepted")
	}
}

func TestLifecycleIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n := time.Now().UnixNano()
	owner, err := workspace.Service{DB: db}.Provision(t.Context(), "invite-owner-"+time.Unix(0, n).Format("150405.000000000")+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := workspace.Service{DB: db}.Provision(t.Context(), "invite-guest-"+time.Unix(0, n).Format("150405.000000000")+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := workspace.Service{DB: db}.Provision(t.Context(), "invite-wrong-"+time.Unix(0, n).Format("150405.000000000")+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id IN($1,$2,$3)`, owner.UserID, guest.UserID, wrong.UserID)
	p, err := project.Service{DB: db}.Create(t.Context(), owner.UserID, owner.OrganizationID, "shared")
	if err != nil {
		t.Fatal(err)
	}
	env, err := project.Service{DB: db}.CreateEnvironment(t.Context(), owner.UserID, p.ID, "production", true)
	if err != nil {
		t.Fatal(err)
	}
	var email string
	if err = db.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1`, guest.UserID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: db}
	i, err := s.Create(t.Context(), owner.UserID, p.ID, env.ID, email, "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Accept(t.Context(), wrong.UserID, i.Token); err != ErrForbidden {
		t.Fatal("wrong user accepted invitation")
	}
	if err = s.Accept(t.Context(), guest.UserID, i.Token); err != nil {
		t.Fatal(err)
	}
	if err = (access.Service{DB: db}).Allow(t.Context(), guest.UserID, env.ID, "read"); err != nil {
		t.Fatal("grant not usable", err)
	}
	if err = s.Accept(t.Context(), guest.UserID, i.Token); err != ErrForbidden {
		t.Fatal("invitation reused")
	}
	projects, err := project.Service{DB: db}.List(t.Context(), guest.UserID)
	if err != nil || len(projects) != 1 || projects[0].ID != p.ID {
		t.Fatal("shared project not listed", err)
	}
}
