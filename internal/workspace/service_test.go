package workspace

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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
	// Identity, not the whole struct: Created differs on purpose between the
	// sign-up and every sign-in after it.
	if first.UserID != second.UserID || first.OrganizationID != second.OrganizationID {
		t.Fatalf("not idempotent: %#v %#v", first, second)
	}
	if !first.Created || second.Created {
		t.Fatalf("Created should be true then false, got %v then %v", first.Created, second.Created)
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
	// Start from nothing: this test is about which caller creates the account,
	// so a row left behind by an earlier run would make it assert nothing.
	if _, err := db.Exec(t.Context(), `DELETE FROM users WHERE email = $1`, email); err != nil {
		t.Fatal(err)
	}
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
	// Exactly one of the racing callers may report having created the account;
	// all of them must get the same workspace back.
	var first Personal
	created := 0
	for p := range out {
		if first.UserID == "" {
			first = p
		}
		if p.UserID != first.UserID || p.OrganizationID != first.OrganizationID {
			t.Fatalf("multiple workspaces: %#v %#v", first, p)
		}
		if p.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d concurrent callers reported creating the account, want exactly 1", created)
	}
	defer db.Exec(t.Context(), `DELETE FROM organizations WHERE id=$1`, first.OrganizationID)
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, first.UserID)
}

// The welcome fires once, for the sign-up that created the account, and never
// again however many times that person signs in afterwards. Greeting somebody
// on every login is the failure this whole mechanism exists to avoid.
func TestOnNewUserFiresOnceOnly(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	var mu sync.Mutex
	greeted := []string{}
	s := Service{DB: db, OnNewUser: func(email string) {
		mu.Lock()
		defer mu.Unlock()
		greeted = append(greeted, email)
	}}

	email := fmt.Sprintf("welcome-%d@example.test", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })

	first, err := s.Provision(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Error("the first Provision did not report the account as created")
	}

	// Three more sign-ins, which is what Identity does on every login.
	for i := 0; i < 3; i++ {
		again, err := s.Provision(t.Context(), email)
		if err != nil {
			t.Fatal(err)
		}
		if again.Created {
			t.Fatalf("sign-in %d reported the account as newly created", i+1)
		}
		if again.UserID != first.UserID {
			t.Fatal("Provision returned a different user for the same address")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(greeted) != 1 {
		t.Fatalf("greeted %d time(s), want exactly 1: %v", len(greeted), greeted)
	}
	if greeted[0] != email {
		t.Errorf("greeted %q, want %q", greeted[0], email)
	}
}

// A provisioning failure must not greet anybody: the announcement happens
// after the commit, so a rolled-back sign-up sends nothing.
func TestNoGreetingWithoutAnAccount(t *testing.T) {
	called := false
	s := Service{DB: nil, OnNewUser: func(string) { called = true }}
	if _, err := s.Provision(t.Context(), "nobody@example.test"); err == nil {
		t.Fatal("provisioned with no database")
	}
	if called {
		t.Error("greeted somebody whose sign-up failed")
	}
}
