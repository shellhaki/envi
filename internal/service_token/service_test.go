package service_token

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/workspace"
	"testing"
	"time"
)

func TestCreateValidation(t *testing.T) {
	s := Service{}
	if _, err := s.Create(context.Background(), "", "", "", "", "read", 0); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := s.Create(context.Background(), "", "", "", "deploy", "invalid", 0); err == nil {
		t.Fatal("invalid permission accepted")
	}
}

func TestLifecycleIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, e := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	w, e := workspace.Service{DB: db}.Provision(t.Context(), "service-token@example.com")
	if e != nil {
		t.Fatal(e)
	}
	p, e := project.Service{DB: db}.Create(t.Context(), w.UserID, w.OrganizationID, "service-token-project")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, w.UserID)
	env, e := project.Service{DB: db}.CreateEnvironment(t.Context(), w.UserID, p.ID, "production", true)
	if e != nil {
		t.Fatal(e)
	}
	s := Service{DB: db}
	token, e := s.Create(t.Context(), w.UserID, p.ID, env.ID, "deploy", "read", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	_, gotEnv, perm, e := s.Authenticate(t.Context(), token.Value)
	if e != nil || gotEnv != env.ID || perm != "read" {
		t.Fatal(gotEnv, perm, e)
	}
	if e = s.Revoke(t.Context(), w.UserID, token.Value); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.Authenticate(t.Context(), token.Value); e == nil {
		t.Fatal("revoked token accepted")
	}
	expired, e := s.Create(t.Context(), w.UserID, p.ID, env.ID, "expired", "read", -time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.Authenticate(t.Context(), expired.Value); e == nil {
		t.Fatal("expired token accepted")
	}
}
