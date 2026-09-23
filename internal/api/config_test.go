package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/apikey"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/workspace"
)

// configFixture builds a real router over a real database with one project,
// two environments, and secrets in each.
type configFixture struct {
	router                *gin.Engine
	db                    *pgxpool.Pool
	userID                string
	projectName           string
	devEnvID, prodEnvID   string
	devValues, prodValues map[string]string
}

func newConfigFixture(t *testing.T) *configFixture {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	suffix := time.Now().UnixNano()
	w, err := workspace.Service{DB: db}.Provision(t.Context(), fmt.Sprintf("config-%d@example.test", suffix))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, w.UserID) })

	projects := project.Service{DB: db}
	name := fmt.Sprintf("config-test-%d", suffix)
	p, err := projects.Create(t.Context(), w.UserID, w.OrganizationID, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM projects WHERE id=$1`, p.ID) })

	dev, err := projects.CreateEnvironment(t.Context(), w.UserID, p.ID, "development", false)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := projects.CreateEnvironment(t.Context(), w.UserID, p.ID, "production", true)
	if err != nil {
		t.Fatal(err)
	}

	cipher, err := crypt.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := secret.Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher}
	devValues := map[string]string{"WHERE": "development", "SHARED": "1"}
	prodValues := map[string]string{"WHERE": "production"}
	if _, err = secrets.PutAll(t.Context(), w.UserID, dev.ID, devValues, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = secrets.PutAll(t.Context(), w.UserID, prod.ID, prodValues, 0); err != nil {
		t.Fatal(err)
	}

	router := Build(db,
		auth.LoginSettings{},
		projects,
		secrets,
		kv.Store{DB: db, Cipher: cipher},
		audit.Service{DB: db},
		service_token.Service{DB: db},
		invitation.Service{DB: db},
		"http://web.test",
		false,
	)
	return &configFixture{
		router: router, db: db, userID: w.UserID, projectName: name,
		devEnvID: dev.ID, prodEnvID: prod.ID, devValues: devValues, prodValues: prodValues,
	}
}

type configReplyBody struct {
	Project     string            `json:"project"`
	Environment string            `json:"environment"`
	Values      map[string]string `json:"values"`
	Revision    int64             `json:"revision"`
	Code        string            `json:"code"`
	Error       string            `json:"error"`
}

func (f *configFixture) get(t *testing.T, query, bearer string) (int, configReplyBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/values"+query, bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	var body configReplyBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

// A session works, and names resolve to the right environment: dev and prod
// hold a key of the same name with different values, so a mix-up is visible.
func TestConfigWithUserSession(t *testing.T) {
	f := newConfigFixture(t)
	session, _, err := auth.StartSession(f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}

	code, body := f.get(t, "?project="+f.projectName+"&environment=development", session)
	if code != 200 || body.Values["WHERE"] != "development" {
		t.Fatalf("development: %d %+v", code, body)
	}
	code, body = f.get(t, "?project="+f.projectName+"&environment=production", session)
	if code != 200 || body.Values["WHERE"] != "production" {
		t.Fatalf("production: %d %+v", code, body)
	}
	if body.Project != f.projectName || body.Environment != "production" {
		t.Fatalf("reply does not echo what was resolved: %+v", body)
	}
}

// A wrong name is the likeliest mistake, so the error has to say what exists.
func TestConfigUnknownNames(t *testing.T) {
	f := newConfigFixture(t)
	session, _, err := auth.StartSession(f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}

	code, body := f.get(t, "?project=no-such-project", session)
	if code != 404 || body.Code != "project_not_found" {
		t.Fatalf("unknown project: %d %+v", code, body)
	}
	code, body = f.get(t, "?project="+f.projectName+"&environment=staging", session)
	if code != 404 || body.Code != "environment_not_found" {
		t.Fatalf("unknown environment: %d %+v", code, body)
	}
	if !contains(body.Error, "development") || !contains(body.Error, "production") {
		t.Fatalf("the error should list the real environments: %q", body.Error)
	}
	// Two environments and none named: ambiguous, so say so rather than guess.
	code, body = f.get(t, "?project="+f.projectName, session)
	if code != 404 || !contains(body.Error, "several environments") {
		t.Fatalf("omitted environment: %d %+v", code, body)
	}
	code, _ = f.get(t, "", session)
	if code != 400 {
		t.Fatalf("omitted project: %d, want 400", code)
	}
}

// A service token needs no names, and must not be able to read past the one
// environment it belongs to.
func TestConfigWithServiceToken(t *testing.T) {
	f := newConfigFixture(t)
	token, err := service_token.Service{DB: f.db}.Create(t.Context(), f.userID, projectOf(t, f), f.devEnvID, "sdk", "read", 0)
	if err != nil {
		t.Fatal(err)
	}

	code, body := f.get(t, "", token.Value)
	if code != 200 || body.Values["WHERE"] != "development" {
		t.Fatalf("service token with no names: %d %+v", code, body)
	}
	if body.Environment != "development" || body.Project != f.projectName {
		t.Fatalf("the reply should name what the token reads: %+v", body)
	}
	// Asserting the wrong environment fails loudly rather than serving dev.
	code, body = f.get(t, "?project="+f.projectName+"&environment=production", token.Value)
	if code != 403 {
		t.Fatalf("token asked for another environment: %d %+v", code, body)
	}
	// And the right one is fine.
	code, _ = f.get(t, "?project="+f.projectName+"&environment=development", token.Value)
	if code != 200 {
		t.Fatalf("token asked for its own environment: %d", code)
	}
}

// A read-only API key reaches config; revoking it closes the door immediately.
func TestConfigWithAPIKey(t *testing.T) {
	f := newConfigFixture(t)
	key, err := apikey.Create(t.Context(), f.db, f.userID, "sdk", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accessToken, _, err := auth.StartLimitedSession(f.db, f.userID, key.Permission, key.ID)
	if err != nil {
		t.Fatal(err)
	}

	code, body := f.get(t, "?project="+f.projectName+"&environment=development", accessToken)
	if code != 200 || body.Values["WHERE"] != "development" {
		t.Fatalf("read-only key: %d %+v", code, body)
	}
	if err = apikey.Revoke(t.Context(), f.db, f.userID, key.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ = f.get(t, "?project="+f.projectName+"&environment=development", accessToken); code != 401 {
		t.Fatalf("after revoking the key: %d, want 401", code)
	}
}

// projectOf finds the fixture's project id, which the service token needs.
func projectOf(t *testing.T, f *configFixture) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(t.Context(), `SELECT project_id FROM environments WHERE id=$1`, f.devEnvID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && bytes.Contains([]byte(haystack), []byte(needle))
}
