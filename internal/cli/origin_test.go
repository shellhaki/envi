package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectctx "shellhaki/envi/internal/cli/project"
)

// originServer fakes the two endpoints the origin commands use, and records
// what was pushed so a test can assert nothing was written.
type originServer struct {
	envs     []map[string]any
	secrets  map[string]map[string]string
	revision map[string]int64
	pushedTo string
	pushed   map[string]string
}

func newOriginServer() *originServer {
	return &originServer{
		envs: []map[string]any{
			{"ID": "e-dev", "ProjectID": "p1", "Name": "development", "Production": false},
			{"ID": "e-prod", "ProjectID": "p1", "Name": "production", "Production": true},
		},
		secrets: map[string]map[string]string{
			"e-dev":  {"API_KEY": "dev-key"},
			"e-prod": {"API_KEY": "prod-key", "ONLY_PROD": "x"},
		},
		revision: map[string]int64{"e-dev": 3, "e-prod": 7},
	}
}

func (s *originServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/projects/p1/environments":
			_ = json.NewEncoder(w).Encode(s.envs)
		case strings.HasSuffix(r.URL.Path, "/secrets/snapshot") && r.Method == "GET":
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/environments/"), "/")[0]
			_ = json.NewEncoder(w).Encode(map[string]any{"values": s.secrets[id], "revision": s.revision[id]})
		case strings.HasSuffix(r.URL.Path, "/secrets/snapshot") && r.Method == "PUT":
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/environments/"), "/")[0]
			var in struct {
				Values           map[string]string `json:"values"`
				ExpectedRevision int64             `json:"expected_revision"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			// The real server compare-and-swaps; mirror that so a wrong
			// revision fails here too.
			if in.ExpectedRevision != s.revision[id] {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "stale_revision", "error": "remote secrets changed"})
				return
			}
			s.pushedTo, s.pushed = id, in.Values
			s.revision[id]++
			s.secrets[id] = in.Values
			_ = json.NewEncoder(w).Encode(map[string]any{"revision": s.revision[id]})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func initDir(t *testing.T, envID, envName string, revision int64, dotenv string) string {
	t.Helper()
	dir := t.TempDir()
	if err := projectctx.Write(dir, projectctx.Context{
		Version:     1,
		Project:     projectctx.Resource{ID: "p1", Name: "demo"},
		Environment: projectctx.Resource{ID: envID, Name: envName, Revision: revision},
	}); err != nil {
		t.Fatal(err)
	}
	if dotenv != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Switching pulls the target's secrets and records its revision. Carrying the
// old origin's revision across would make the next push compare against the
// wrong number.
func TestSwitchOriginPullsAndResetsRevision(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=dev-key\n")

	var out bytes.Buffer
	c := Client{BaseURL: srv.URL, Token: "t"}
	if err := SwitchOrigin(context.Background(), c, strings.NewReader(""), &out, dir, "production", false); err != nil {
		t.Fatal(err)
	}
	ctx, err := projectctx.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Environment.ID != "e-prod" || ctx.Environment.Name != "production" {
		t.Fatalf("context still on %+v", ctx.Environment)
	}
	if ctx.Environment.Revision != 7 {
		t.Fatalf("revision = %d, want the target's 7", ctx.Environment.Revision)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(b), "prod-key") || !strings.Contains(string(b), "ONLY_PROD") {
		t.Fatalf(".env was not replaced with the target's secrets:\n%s", b)
	}
	if !strings.Contains(out.String(), "production") {
		t.Fatalf("expected the warning that production is production, got %q", out.String())
	}
}

// Unpushed local edits would be destroyed by the pull, so the switch refuses.
func TestSwitchOriginRefusesWithLocalChanges(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=edited-locally\n")

	var out bytes.Buffer
	err := SwitchOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, "production", false)
	if err == nil || !strings.Contains(err.Error(), "not pushed") {
		t.Fatalf("expected a refusal about unpushed changes, got %v", err)
	}
	ctx, _ := projectctx.Load(dir)
	if ctx.Environment.ID != "e-dev" {
		t.Fatal("context moved despite the refusal")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(b), "edited-locally") {
		t.Fatal("local edit was overwritten despite the refusal")
	}
}

func TestSwitchOriginForceDiscardsLocalChanges(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=edited-locally\n")

	var out bytes.Buffer
	if err := SwitchOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, "production", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if strings.Contains(string(b), "edited-locally") {
		t.Fatal("--force did not replace .env")
	}
}

func TestSwitchOriginUnknownNameListsTheRealOnes(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=dev-key\n")
	var out bytes.Buffer
	err := SwitchOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, "staging", false)
	if err == nil || !strings.Contains(err.Error(), "development, production") {
		t.Fatalf("expected the available origins in the error, got %v", err)
	}
}

// Answering anything but yes must leave the target untouched.
func TestPushToOriginRequiresConfirmation(t *testing.T) {
	for _, answer := range []string{"", "n\n", "no\n", "nope\n"} {
		s := newOriginServer()
		srv := s.start(t)
		dir := initDir(t, "e-dev", "development", 3, "API_KEY=dev-key\nNEW=1\n")
		var out bytes.Buffer
		if err := PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(answer), &out, dir, "", "production", false, false); err != nil {
			t.Fatal(err)
		}
		if s.pushedTo != "" {
			t.Fatalf("answer %q pushed anyway", answer)
		}
		if !strings.Contains(out.String(), "Cancelled") {
			t.Fatalf("answer %q: expected a cancellation notice, got %q", answer, out.String())
		}
	}
}

func TestPushToOriginAcceptsYesAndUsesTargetRevision(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "YES\n"} {
		s := newOriginServer()
		srv := s.start(t)
		dir := initDir(t, "e-dev", "development", 3, "API_KEY=dev-key\nNEW=1\n")
		var out bytes.Buffer
		if err := PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(answer), &out, dir, "", "production", false, false); err != nil {
			t.Fatalf("answer %q: %v", answer, err)
		}
		// The fake server 409s on a wrong expected_revision, so reaching here
		// proves the target's revision was used, not the current origin's 3.
		if s.pushedTo != "e-prod" {
			t.Fatalf("answer %q pushed to %q, want e-prod", answer, s.pushedTo)
		}
		if s.pushed["NEW"] != "1" || s.pushed["API_KEY"] != "dev-key" {
			t.Fatalf("pushed the wrong values: %v", s.pushed)
		}
	}
}

// The prompt has to say what it is about to do, including that keys go away.
func TestPushToOriginShowsTheDiffBeforeAsking(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=changed\nNEW=1\n")
	var out bytes.Buffer
	_ = PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader("n\n"), &out, dir, "", "production", false, false)
	for _, want := range []string{"add 1", "NEW", "change 1", "API_KEY", "remove 1", "ONLY_PROD", "production"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompt missing %q:\n%s", want, out.String())
		}
	}
}

func TestPushToOriginNoOpWhenIdentical(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "API_KEY=prod-key\nONLY_PROD=x\n")
	var out bytes.Buffer
	if err := PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, "", "production", false, false); err != nil {
		t.Fatal(err)
	}
	if s.pushedTo != "" {
		t.Fatal("pushed even though nothing differed")
	}
	if !strings.Contains(out.String(), "already matches") {
		t.Fatalf("got %q", out.String())
	}
}

func TestListOriginsMarksCurrent(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-prod", "production", 7, "")
	var out bytes.Buffer
	if err := ListOrigins(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, dir, &out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		marked := strings.HasPrefix(line, "*")
		if strings.Contains(line, "production") != marked {
			t.Fatalf("wrong line marked as current: %q", out.String())
		}
	}
}

// envi push .env.test origin prod — the file is read from disk by name, and
// --force answers the prompt so the whole thing is one non-interactive command.
func TestPushToOriginNamedFileAndForce(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "FROM=dotenv\n")
	if err := os.WriteFile(filepath.Join(dir, ".env.test"), []byte("FROM=test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, ".env.test", "production", false, true); err != nil {
		t.Fatal(err)
	}
	if s.pushed["FROM"] != "test" {
		t.Fatalf("pushed %v, want the named file's values", s.pushed)
	}
	if !strings.Contains(out.String(), ".env.test") {
		t.Fatalf("output never names the file it pushed:\n%s", out.String())
	}
}

func TestPushToOriginMissingNamedFile(t *testing.T) {
	s := newOriginServer()
	srv := s.start(t)
	dir := initDir(t, "e-dev", "development", 3, "FROM=dotenv\n")
	var out bytes.Buffer
	err := PushToOrigin(context.Background(), Client{BaseURL: srv.URL, Token: "t"}, strings.NewReader(""), &out, dir, ".env.nope", "production", true, false)
	if err == nil || !strings.Contains(err.Error(), ".env.nope") {
		t.Fatalf("got %v, expected an error naming the missing file", err)
	}
}
