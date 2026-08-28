package secret

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/audit"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/workspace"
	"sync"
	"testing"
)

func TestSecretAccessAuditIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, e := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	w, e := workspace.Service{DB: db}.Provision(t.Context(), "secret-owner@example.com")
	if e != nil {
		t.Fatal(e)
	}
	p, e := project.Service{DB: db}.Create(t.Context(), w.UserID, w.OrganizationID, "secret-test")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Exec(t.Context(), `DELETE FROM projects WHERE id=$1`, p.ID)
	ps := project.Service{DB: db}
	dev, e := ps.CreateEnvironment(t.Context(), w.UserID, p.ID, "dev", false)
	if e != nil {
		t.Fatal(e)
	}
	prod, e := ps.CreateEnvironment(t.Context(), w.UserID, p.ID, "prod", true)
	if e != nil {
		t.Fatal(e)
	}
	c, _ := crypt.New([]byte("01234567890123456789012345678901"))
	s := Service{DB: db, Access: access.Service{DB: db}, Cipher: c}
	if e = s.Put(t.Context(), w.UserID, dev.ID, "API_KEY", "plaintext-value"); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(t.Context(), w.UserID, dev.ID)
	if e != nil || got["API_KEY"] != "plaintext-value" {
		t.Fatal(e)
	}
	var raw []byte
	if e = db.QueryRow(t.Context(), `SELECT ciphertext FROM secret_versions WHERE secret_id IN(SELECT id FROM secrets WHERE environment_id=$1) LIMIT 1`, dev.ID).Scan(&raw); e != nil || string(raw) == "plaintext-value" {
		t.Fatal("plaintext stored")
	}
	if e = s.Put(t.Context(), w.UserID, prod.ID, "API_KEY", "prod"); e != access.ErrForbidden {
		t.Fatal("production allowed without grant")
	}
	_, e = db.Exec(t.Context(), `INSERT INTO access_grants(subject_user_id,project_id,environment_id,permission)VALUES($1,$2,$3,'write')`, w.UserID, p.ID, prod.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Put(t.Context(), w.UserID, prod.ID, "API_KEY", "prod"); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(t.Context(), w.UserID, dev.ID, "API_KEY"); e != nil {
		t.Fatal(e)
	}
	if got, e = s.Get(t.Context(), w.UserID, dev.ID); e != nil || len(got) != 0 {
		t.Fatal("deleted secret returned")
	}
	var versions int
	if e = db.QueryRow(t.Context(), `SELECT count(*) FROM secret_versions v JOIN secrets s ON s.id=v.secret_id WHERE s.environment_id=$1`, dev.ID).Scan(&versions); e != nil || versions == 0 {
		t.Fatal("versions removed")
	}
	var events int
	if e = db.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action LIKE 'secret.%'`, w.UserID).Scan(&events); e != nil || events < 3 {
		t.Fatalf("events=%d %v", events, e)
	}
	if got, e := (audit.Service{DB: db}).List(t.Context(), w.UserID, w.OrganizationID); e != nil || len(got) < 3 {
		t.Fatalf("audit list=%d %v", len(got), e)
	}
	other, e := workspace.Service{DB: db}.Provision(t.Context(), "secret-audit-other@example.com")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (audit.Service{DB: db}).List(t.Context(), other.UserID, w.OrganizationID); e != audit.ErrForbidden {
		t.Fatal("unauthorized audit access")
	}
}

func TestConcurrentPushIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, e := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	w, e := workspace.Service{DB: db}.Provision(t.Context(), "secret-conflict@example.com")
	if e != nil {
		t.Fatal(e)
	}
	p, e := project.Service{DB: db}.Create(t.Context(), w.UserID, w.OrganizationID, "secret-conflict")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Exec(t.Context(), `DELETE FROM projects WHERE id=$1`, p.ID)
	env, e := (project.Service{DB: db}).CreateEnvironment(t.Context(), w.UserID, p.ID, "dev", false)
	if e != nil {
		t.Fatal(e)
	}
	cipher, _ := crypt.New([]byte("01234567890123456789012345678901"))
	s := Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, value := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.PutAll(t.Context(), w.UserID, env.ID, map[string]string{"KEY": value}, 0)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	var succeeded, conflicted int
	for err := range errs {
		if err == nil {
			succeeded++
		} else if err == ErrConflict {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}
