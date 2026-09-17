package project

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"shellhaki/envi/internal/workspace"
	"testing"
)

func TestProjectEnvironmentIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, e := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	w, e := workspace.Service{DB: db}.Provision(t.Context(), "project-owner@example.com")
	if e != nil {
		t.Fatal(e)
	}
	s := Service{DB: db}
	p, e := s.Create(t.Context(), w.UserID, w.OrganizationID, "demo")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(t.Context(), w.UserID, w.OrganizationID, "demo"); e == nil {
		t.Fatal("duplicate accepted")
	}
	env, e := s.CreateEnvironment(t.Context(), w.UserID, p.ID, "production", true)
	if e != nil || !env.Production {
		t.Fatal(e)
	}
	env, e = s.UpdateEnvironment(t.Context(), w.UserID, env.ID, "prod", false)
	if e != nil || env.Production {
		t.Fatal(e)
	}
	other, e := workspace.Service{DB: db}.Provision(t.Context(), "project-other@example.com")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ListEnvironments(t.Context(), other.UserID, p.ID); e != ErrForbidden {
		t.Fatal("cross-user access allowed")
	}
	if e = s.DeleteEnvironment(t.Context(), w.UserID, env.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateEnvironment(t.Context(), w.UserID, p.ID, "cascade-check", false); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(t.Context(), other.UserID, p.ID); e != ErrForbidden {
		t.Fatal("a user outside the org deleted the project")
	}
	if e = s.Delete(t.Context(), w.UserID, p.ID); e != nil {
		t.Fatal(e)
	}
	var remaining int
	if e = db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM projects WHERE id=$1)+(SELECT count(*) FROM environments WHERE project_id=$1)`, p.ID).Scan(&remaining); e != nil || remaining != 0 {
		t.Fatalf("project or its environments survived deletion: %d rows (%v)", remaining, e)
	}
	_, _ = db.Exec(t.Context(), `DELETE FROM users WHERE id IN($1,$2)`, w.UserID, other.UserID)
}
