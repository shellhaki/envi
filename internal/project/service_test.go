package project

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/workspace"
)

// A project with no environment cannot be linked by envi init, so creating one
// must create its first environment too.
func TestCreateMakesADefaultEnvironment(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	w, err := workspace.Service{DB: db}.Provision(t.Context(), fmt.Sprintf("project-%d@example.test", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, w.UserID)

	s := Service{DB: db}
	p, err := s.Create(t.Context(), w.UserID, w.OrganizationID, fmt.Sprintf("proj-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM projects WHERE id=$1`, p.ID)

	envs, err := s.ListEnvironments(t.Context(), w.UserID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 {
		t.Fatalf("new project has %d environments, want 1", len(envs))
	}
	if envs[0].Name != DefaultEnvironmentName {
		t.Fatalf("first environment is %q, want %q", envs[0].Name, DefaultEnvironmentName)
	}
	if envs[0].Production {
		t.Fatal("the default environment must not be production, or members need a grant to use it")
	}
}

// A rejected project must leave nothing behind.
func TestCreateRollsBackTogether(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	w, err := workspace.Service{DB: db}.Provision(t.Context(), fmt.Sprintf("rollback-%d@example.test", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, w.UserID)

	s := Service{DB: db}
	name := fmt.Sprintf("dup-%d", time.Now().UnixNano())
	p, err := s.Create(t.Context(), w.UserID, w.OrganizationID, name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM projects WHERE id=$1`, p.ID)

	if _, err = s.Create(t.Context(), w.UserID, w.OrganizationID, name); err == nil {
		t.Fatal("a duplicate project name was accepted")
	}
	var orphans int
	if err = db.QueryRow(t.Context(),
		`SELECT count(*) FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.org_id=$1`, w.OrganizationID).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 1 {
		t.Fatalf("%d environments after one successful and one failed create, want 1", orphans)
	}
}
