package workspace

import (
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProvisionIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	email := " Workspace-Test@Example.com "
	s := Service{DB: db}
	first, err := s.Provision(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Provision(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("not idempotent: %#v %#v", first, second)
	}
	var role, stored string
	if err = db.QueryRow(t.Context(), `SELECT m.role,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1`, first.OrganizationID).Scan(&role, &stored); err != nil {
		t.Fatal(err)
	}
	if role != "owner" || stored != "workspace-test@example.com" {
		t.Fatalf("role=%s email=%s", role, stored)
	}
	defer db.Exec(t.Context(), `DELETE FROM organizations WHERE id=$1`, first.OrganizationID)
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, first.UserID)
}

func TestProvisionConcurrentIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Service{DB: db}
	email := "workspace-concurrent@example.com"
	var wg sync.WaitGroup
	out := make(chan Personal, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p, e := s.Provision(t.Context(), email); out <- p; errs <- e }()
	}
	wg.Wait()
	close(out)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first Personal
	for p := range out {
		if first == (Personal{}) {
			first = p
		}
		if p != first {
			t.Fatalf("multiple workspaces: %#v %#v", first, p)
		}
	}
	defer db.Exec(t.Context(), `DELETE FROM organizations WHERE id=$1`, first.OrganizationID)
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, first.UserID)
}
