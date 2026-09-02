// Package e2e exercises the compiled CLI surface against a real API server.
//
// The test boots the production wiring (api.Build) on an httptest server backed
// by a live Postgres and Redis, but swaps the Gmail mailer for a capturing one
// so authentication never sends a real email: the OTP is read off a channel and
// fed to the CLI's stdin. It then drives the injectable cli.App through the full
// lifecycle with fake secrets and cleans up every row it creates.
//
// Gated on ENVI_INTEGRATION=1 with DATABASE_URL and REDIS_URL set, matching the
// other integration tests in this repo.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/api"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/cli"
	"shellhaki/envi/internal/cli/session"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/otp"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/workspace"
)

// capturingMailer records the OTP instead of emailing it, so the auth flow can
// run without SMTP. codes is buffered so Send never blocks the request handler.
type capturingMailer struct{ codes chan string }

func (m capturingMailer) Send(_, code string) error {
	m.codes <- code
	return nil
}

// memStore is an in-memory TokenStore, replacing the on-disk sqlite session so
// the test leaves nothing behind and can run several independent sessions.
type memStore struct {
	mu  sync.Mutex
	tok cli.Tokens
	set bool
}

func (s *memStore) Save(t cli.Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok, s.set = t, true
	return nil
}
func (s *memStore) Load() (cli.Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.set {
		return cli.Tokens{}, session.ErrNotFound
	}
	return s.tok, nil
}
func (s *memStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok, s.set = cli.Tokens{}, false
	return nil
}

func TestCLIEndToEndIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("REDIS_URL") == "" {
		t.Skip("DATABASE_URL and REDIS_URL are required")
	}
	// Guarantee a clean CLI environment regardless of the ambient shell: a stray
	// ENVI_TOKEN would make authorize() bypass the session store entirely.
	t.Setenv("ENVI_TOKEN", "")
	t.Setenv("ENVI_API_URL", "")
	t.Setenv("ENVI_CONFIG_DIR", t.TempDir())

	ctx := t.Context()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		t.Fatalf("postgres unavailable: %v", err)
	}

	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	rc := redis.NewClient(opt)
	defer rc.Close()
	if err = rc.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis unavailable: %v", err)
	}

	// Best-effort teardown of everything created below. Registered after
	// db.Close so it runs first (LIFO), and uses a background context because the
	// test context is cancelled during cleanup.
	var ownerID, ownerOrg, collabID, collabOrg, projectID string
	defer func() {
		bg := context.Background()
		if projectID != "" {
			db.Exec(bg, `DELETE FROM projects WHERE id=$1`, projectID)
		}
		for _, u := range []string{ownerID, collabID} {
			if u != "" {
				db.Exec(bg, `DELETE FROM access_grants WHERE subject_user_id=$1`, u)
				db.Exec(bg, `DELETE FROM memberships WHERE user_id=$1`, u)
				db.Exec(bg, `DELETE FROM device_authorizations WHERE user_id=$1`, u)
			}
		}
		for _, o := range []string{ownerOrg, collabOrg} {
			if o != "" {
				db.Exec(bg, `DELETE FROM organizations WHERE id=$1`, o)
			}
		}
		for _, u := range []string{ownerID, collabID} {
			if u != "" {
				db.Exec(bg, `DELETE FROM users WHERE id=$1`, u)
			}
		}
	}()

	const (
		accessTTL  = 15 * time.Minute
		refreshTTL = 30 * 24 * time.Hour
	)
	mailer := capturingMailer{codes: make(chan string, 8)}
	w := workspace.Service{DB: db}
	authSvc := auth.Service{
		OTP:        otp.Service{Store: otp.Redis{Client: rc}, TTL: 10 * time.Minute, MaxAttempts: 10, RequestLimit: 20},
		Mailer:     mailer,
		Provision:  w.Identity,
		AccessTTL:  accessTTL,
		RefreshTTL: refreshTTL,
	}
	cipher, err := crypt.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	tokens := &auth.PostgresTokens{DB: db, AccessTTL: accessTTL, RefreshTTL: refreshTTL}
	deviceSvc := auth.DeviceService{Store: &auth.PostgresDeviceStore{DB: db}, Tokens: tokens, TTL: 10 * time.Minute, Interval: time.Second}
	router := api.Build(
		authSvc, tokens,
		project.Service{DB: db},
		secret.Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher},
		audit.Service{DB: db},
		service_token.Service{DB: db},
		invitation.Service{DB: db},
		db,
		deviceSvc, "http://web.test", accessTTL,
	)
	ts := httptest.NewServer(router)
	defer ts.Close()
	base := ts.URL

	suffix := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("cli-owner-%d@example.test", suffix)
	collabEmail := fmt.Sprintf("cli-collab-%d@example.test", suffix)
	projName := fmt.Sprintf("cli-e2e-%d", suffix)

	// --- auth: owner ---------------------------------------------------------
	ownerStore := &memStore{}
	authenticate(t, base, db, ownerStore, mailer, ownerEmail)
	ownerTok, err := ownerStore.Load()
	if err != nil {
		t.Fatalf("owner session not stored: %v", err)
	}
	ownerAccess := ownerTok.Access

	// --- account + provisioning of a project and two environments (as the web
	// UI would); the CLI has no project-create command, so we call the API. ---
	var me struct{ ID, Email, OrganizationID string }
	if code := apiCall(t, base, "GET", "/me", ownerAccess, nil, &me); code != 200 {
		t.Fatalf("/me returned %d", code)
	}
	ownerID, ownerOrg = me.ID, me.OrganizationID

	var proj struct{ ID, OrgID, Name string }
	if code := apiCall(t, base, "POST", "/projects", ownerAccess,
		map[string]any{"org_id": me.OrganizationID, "name": projName}, &proj); code != 201 {
		t.Fatalf("create project returned %d", code)
	}
	projectID = proj.ID
	var prodEnvID string
	for _, env := range []struct {
		name string
		prod bool
	}{{"dev", false}, {"prod", true}} {
		var out struct{ ID string }
		if code := apiCall(t, base, "POST", "/projects/"+proj.ID+"/environments", ownerAccess,
			map[string]any{"name": env.name, "is_production": env.prod}, &out); code != 201 {
			t.Fatalf("create environment %q returned %d", env.name, code)
		}
		if env.prod {
			prodEnvID = out.ID
		}
	}

	// Regression: an org owner must be able to read and write secrets in a
	// PRODUCTION environment of their own project with no explicit grant. The
	// access check's membership fallback deliberately excludes production, so
	// owners/admins have to be allowed through by role — otherwise the person who
	// created the environment is locked out of it.
	if code := apiCall(t, base, "PUT", "/environments/"+prodEnvID+"/secrets/snapshot", ownerAccess,
		map[string]any{"values": map[string]string{"PROD_ONLY": "v1"}, "expected_revision": 0}, nil); code != 200 {
		t.Fatalf("owner write to production returned %d, want 200", code)
	}
	var prodSnap struct {
		Values map[string]string `json:"values"`
	}
	if code := apiCall(t, base, "GET", "/environments/"+prodEnvID+"/secrets/snapshot", ownerAccess, nil, &prodSnap); code != 200 {
		t.Fatalf("owner read of production returned %d, want 200", code)
	}
	if prodSnap.Values["PROD_ONLY"] != "v1" {
		t.Fatalf("production secret round-trip failed: %+v", prodSnap.Values)
	}

	// From here the file-based commands operate on the current directory.
	ownerDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	if err = os.Chdir(ownerDir); err != nil {
		t.Fatal(err)
	}

	// --- init ---------------------------------------------------------------
	if code, out := runCLI(t, base, ownerStore, "init", "--project", projName, "--env", "dev"); code != 0 || !strings.Contains(out, "Initialized") {
		t.Fatalf("init exit=%d out=%q", code, out)
	}

	// --- push (fake secrets) ------------------------------------------------
	const originalEnv = "API_KEY=sk_test_FAKE_do_not_use\nDB_PASSWORD=hunter2\nFEATURE_X=true\n"
	writeFile(t, filepath.Join(ownerDir, ".env"), originalEnv)
	if code, out := runCLI(t, base, ownerStore, "push"); code != 0 || !strings.Contains(out, "push complete") {
		t.Fatalf("push exit=%d out=%q", code, out)
	}

	// --- pull round-trips the secrets back ----------------------------------
	mustRemove(t, filepath.Join(ownerDir, ".env"))
	if code, out := runCLI(t, base, ownerStore, "pull"); code != 0 || !strings.Contains(out, "pull complete") {
		t.Fatalf("pull exit=%d out=%q", code, out)
	}
	assertEnvContains(t, filepath.Join(ownerDir, ".env"),
		"API_KEY=sk_test_FAKE_do_not_use", "DB_PASSWORD=hunter2", "FEATURE_X=true")

	// --- diff: clean, then with local edits ---------------------------------
	if code, out := runCLI(t, base, ownerStore, "diff"); code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("expected a clean diff, exit=%d out=%q", code, out)
	}
	const modifiedEnv = "API_KEY=sk_test_CHANGED\nDB_PASSWORD=hunter2\nFEATURE_X=true\nNEW_KEY=added_value\n"
	writeFile(t, filepath.Join(ownerDir, ".env"), modifiedEnv)
	if code, out := runCLI(t, base, ownerStore, "diff"); code != 0 ||
		!strings.Contains(out, "changed API_KEY") || !strings.Contains(out, "added NEW_KEY") {
		t.Fatalf("diff did not report local edits, exit=%d out=%q", code, out)
	}

	// --- token create -------------------------------------------------------
	code, out := runCLI(t, base, ownerStore, "token", "create", "--name", "ci-bot", "--permission", "read")
	if code != 0 {
		t.Fatalf("token create exit=%d out=%q", code, out)
	}
	serviceToken := strings.TrimSpace(out)
	if len(serviceToken) < 16 {
		t.Fatalf("service token looks invalid: %q", out)
	}

	// --- share (invite a collaborator to the dev environment) ---------------
	code, out = runCLI(t, base, ownerStore, "share", collabEmail, "--permission", "read")
	if code != 0 {
		t.Fatalf("share exit=%d out=%q", code, out)
	}
	inviteToken := strings.TrimSpace(out)
	if len(inviteToken) < 16 {
		t.Fatalf("invitation token looks invalid: %q", out)
	}

	// --- collaborator: auth, accept the invitation, then read the shared env -
	collabStore := &memStore{}
	authenticate(t, base, db, collabStore, mailer, collabEmail)
	collabTok, err := collabStore.Load()
	if err != nil {
		t.Fatalf("collaborator session not stored: %v", err)
	}
	var cme struct{ ID, Email, OrganizationID string }
	if code := apiCall(t, base, "GET", "/me", collabTok.Access, nil, &cme); code != 200 {
		t.Fatalf("collaborator /me returned %d", code)
	}
	collabID, collabOrg = cme.ID, cme.OrganizationID

	if code, out := runCLI(t, base, collabStore, "invite", "accept", inviteToken); code != 0 {
		t.Fatalf("invite accept exit=%d out=%q", code, out)
	}

	collabDir := t.TempDir()
	if err = os.Chdir(collabDir); err != nil {
		t.Fatal(err)
	}
	if code, out := runCLI(t, base, collabStore, "init", "--project", projName, "--env", "dev"); code != 0 {
		t.Fatalf("collaborator init exit=%d out=%q (grant may not have applied)", code, out)
	}
	if code, out := runCLI(t, base, collabStore, "pull"); code != 0 {
		t.Fatalf("collaborator pull exit=%d out=%q", code, out)
	}
	assertEnvContains(t, filepath.Join(collabDir, ".env"), "API_KEY=sk_test_FAKE_do_not_use")

	// --- service token: pull with a static ENVI_TOKEN (no session) ----------
	if err = os.Chdir(ownerDir); err != nil {
		t.Fatal(err)
	}
	mustRemove(t, filepath.Join(ownerDir, ".env"))
	t.Setenv("ENVI_TOKEN", serviceToken)
	if code, out := runCLI(t, base, &memStore{}, "pull"); code != 0 {
		t.Fatalf("service-token pull exit=%d out=%q", code, out)
	}
	assertEnvContains(t, filepath.Join(ownerDir, ".env"), "API_KEY=sk_test_FAKE_do_not_use")
	t.Setenv("ENVI_TOKEN", "")

	// --- logout clears the local session -----------------------------------
	if code, out := runCLI(t, base, ownerStore, "logout"); code != 0 || !strings.Contains(out, "Signed out") {
		t.Fatalf("logout exit=%d out=%q", code, out)
	}
	if _, err := ownerStore.Load(); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("session survived logout: %v", err)
	}
}

// authenticate drives the browser device flow headlessly. It first establishes a
// web session for email via the OTP path (as the website does), then runs
// `envi auth --no-browser` in the background and approves the pending user code
// with that web session — exactly what a signed-in browser does at /device. The
// CLI itself never sees the OTP; it only polls until the code is approved.
func authenticate(t *testing.T, base string, db *pgxpool.Pool, store cli.TokenStore, mailer capturingMailer, email string) {
	t.Helper()
	// 1. A web session for this user: request-otp, capture the code, verify-otp.
	if code := apiCall(t, base, "POST", "/auth/request-otp", "", map[string]string{"email": email}, nil); code != 202 {
		t.Fatalf("request-otp for %s returned %d", email, code)
	}
	var web struct {
		Access string `json:"access_token"`
	}
	select {
	case otpCode := <-mailer.codes:
		if code := apiCall(t, base, "POST", "/auth/verify-otp", "", map[string]string{"email": email, "code": otpCode}, &web); code != 200 {
			t.Fatalf("verify-otp for %s returned %d", email, code)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("OTP for %s was never delivered", email)
	}
	if web.Access == "" {
		t.Fatalf("no web session token for %s", email)
	}

	// 2. Run the CLI device login; it has no credentials and just polls.
	out := &bytes.Buffer{}
	app := cli.App{Out: out, Err: out, In: strings.NewReader(""), Store: store, Client: cli.Client{BaseURL: base}, Version: "e2e"}
	done := make(chan int, 1)
	go func() { done <- app.Run([]string{"auth", "--no-browser"}) }()

	// 3. Approve the newest pending code with the web session, as /device does.
	userCode := waitForPendingCode(t, db)
	if code := apiCall(t, base, "POST", "/auth/device/approve", web.Access, map[string]string{"user_code": userCode}, nil); code != 204 {
		t.Fatalf("device approve for %s returned %d", email, code)
	}

	select {
	case exit := <-done:
		if exit != 0 || !strings.Contains(out.String(), "Authenticated") {
			t.Fatalf("auth for %s exit=%d out=%q", email, exit, out.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("device auth for %s did not finish; out=%q", email, out.String())
	}
}

// waitForPendingCode returns the most recently created pending device code, giving
// the backgrounded CLI a moment to request one before we approve it.
func waitForPendingCode(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var code string
		err := db.QueryRow(context.Background(),
			`SELECT user_code FROM device_authorizations WHERE status='pending' ORDER BY created_at DESC LIMIT 1`).Scan(&code)
		if err == nil && code != "" {
			return code
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no pending device code appeared")
	return ""
}

// runCLI runs one CLI command with an injected store/client and no interactive
// stdin, returning the exit code and combined stdout+stderr.
func runCLI(t *testing.T, base string, store cli.TokenStore, args ...string) (int, string) {
	t.Helper()
	out := &bytes.Buffer{}
	app := cli.App{Out: out, Err: out, In: strings.NewReader(""), Store: store, Client: cli.Client{BaseURL: base}, Version: "e2e"}
	return app.Run(args), out.String()
}

// apiCall performs a direct authenticated request, used to stand in for the web
// UI when creating the project and environments the CLI later selects.
func apiCall(t *testing.T, base, method, path, bearer string, body, out any) int {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err = json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return res.StatusCode
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertEnvContains(t *testing.T, path string, wants ...string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range wants {
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s missing %q; got:\n%s", path, want, b)
		}
	}
}
