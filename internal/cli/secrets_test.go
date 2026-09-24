package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	projectctx "shellhaki/envi/internal/cli/project"
)

func TestPullPush(t *testing.T) {
	d := t.TempDir()
	if e := projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}}); e != nil {
		t.Fatal(e)
	}
	var pushed string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing auth")
		}
		if r.Method == "GET" {
			w.Write([]byte(`{"values":{"B":"2","A":"1"},"revision":4}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		pushed = string(b)
		w.Write([]byte(`{"revision":5}`))
	}))
	defer s.Close()
	c := Client{BaseURL: s.URL, Token: "token"}
	if n, e := Pull(context.Background(), c, d); e != nil || n != 2 {
		t.Fatalf("pulled %d secrets: %v", n, e)
	}
	b, e := os.ReadFile(filepath.Join(d, ".env"))
	if e != nil || string(b) != "A=1\nB=2\n" {
		t.Fatalf("%q %v", b, e)
	}
	info, _ := os.Stat(filepath.Join(d, ".env"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if n, e := Push(context.Background(), c, d, "", false, false); e != nil || n != 2 {
		t.Fatalf("pushed %d secrets: %v", n, e)
	}
	if !strings.Contains(pushed, `"A":"1"`) || !strings.Contains(pushed, `"B":"2"`) {
		t.Fatal(pushed)
	}
	x, _ := projectctx.Load(d)
	if x.Environment.Revision != 5 || !strings.Contains(pushed, `"expected_revision":4`) {
		t.Fatal("revision not advanced", pushed)
	}
}

func TestDiff(t *testing.T) {
	d := t.TempDir()
	_ = projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}})
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("ADDED=1\nCHANGED=local\nSAME=x\n"), 0600)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"values":{"CHANGED":"remote","REMOVED":"1","SAME":"x"},"revision":2}`))
	}))
	defer s.Close()
	var out strings.Builder
	if e := Diff(context.Background(), Client{BaseURL: s.URL}, d, &out); e != nil {
		t.Fatal(e)
	}
	if out.String() != "added ADDED\nchanged CHANGED\nremoved REMOVED\n" {
		t.Fatal(out.String())
	}
}
func TestPushErrors(t *testing.T) {
	d := t.TempDir()
	if _, e := Push(context.Background(), Client{}, d, "", false, false); e == nil {
		t.Fatal("missing config accepted")
	}
	_ = projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}})
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("bad"), 0600)
	if _, e := Push(context.Background(), Client{}, d, "", false, false); e == nil {
		t.Fatal("malformed env accepted")
	}
}

// Round-trip the values a real Worker secret actually holds: private keys with
// newlines, JSON blobs, connection strings with '=' and '#'.
func TestEnvRoundTripRealisticSecrets(t *testing.T) {
	cases := map[string]map[string]string{
		"plain":            {"API_KEY": "sk_live_abc123"},
		"spaces":           {"GREETING": "hello world"},
		"equals in value":  {"DATABASE_URL": "postgres://u:p@h/db?a=1&b=2"},
		"hash in value":    {"COLOR": "#ff0000"},
		"quotes in value":  {"JSON_BLOB": `{"a":"b"}`},
		"trailing space":   {"PADDED": "value "},
		"empty value":      {"EMPTY": ""},
		"newline in value": {"PRIVATE_KEY": "-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, ".env")
			if err := writeEnv(p, in); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, err := parseEnv(f)
			if err != nil {
				raw, _ := os.ReadFile(p)
				t.Fatalf("parse failed: %v\nfile written was:\n%s", err, raw)
			}
			if !reflect.DeepEqual(got, in) {
				raw, _ := os.ReadFile(p)
				t.Fatalf("round-trip changed the value\n  wrote: %q\n  read:  %q\n  file:\n%s",
					in, got, strings.ReplaceAll(string(raw), "\n", "\\n\n"))
			}
		})
	}
}

// ctxDir is a project directory already initialized against env "e".
func ctxDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev", Revision: 4}}); e != nil {
		t.Fatal(e)
	}
	return d
}

// snapshotServer answers GET with the revision it holds and accepts a PUT only
// when expected_revision matches it — the server's real compare-and-swap.
func snapshotServer(t *testing.T, revision int64, sent *map[string]any) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprintf(w, `{"values":{"REMOTE":"1"},"revision":%d}`, revision)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if int64(body["expected_revision"].(float64)) != revision {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "stale_revision", "error": "remote secrets changed"})
			return
		}
		*sent = body
		fmt.Fprintf(w, `{"revision":%d}`, revision+1)
	}))
	t.Cleanup(s.Close)
	return s
}

// The point of --force: the remote moved on, the local file is what we want,
// and pulling first would only bring down secrets we are about to discard.
func TestPushForceOverwritesAStaleRevision(t *testing.T) {
	d := ctxDir(t)
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("LOCAL=1\n"), 0600)
	var sent map[string]any
	c := Client{BaseURL: snapshotServer(t, 9, &sent).URL}

	if _, e := Push(context.Background(), c, d, "", false, false); e == nil {
		t.Fatal("a stale revision was accepted without --force")
	}
	if _, e := Push(context.Background(), c, d, "", true, false); e != nil {
		t.Fatalf("--force did not get past the stale revision: %v", e)
	}
	if got := sent["expected_revision"]; got != float64(9) {
		t.Fatalf("pushed against revision %v, want the server's current 9", got)
	}
	if x, _ := projectctx.Load(d); x.Environment.Revision != 10 {
		t.Fatalf("envi.toml kept revision %d after a forced push", x.Environment.Revision)
	}
}

// The server's own wording sends people to envi pull, which is the thing
// --force exists to avoid; the CLI must offer both ways out.
func TestStaleRevisionTellsYouAboutForce(t *testing.T) {
	d := ctxDir(t)
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("LOCAL=1\n"), 0600)
	var sent map[string]any
	_, e := Push(context.Background(), Client{BaseURL: snapshotServer(t, 9, &sent).URL}, d, "", false, false)
	if e == nil || !strings.Contains(e.Error(), "--force") {
		t.Fatalf("stale revision reported as %v, expected it to mention --force", e)
	}
}

func TestPushNamedFile(t *testing.T) {
	d := ctxDir(t)
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("FROM=dotenv\n"), 0600)
	_ = os.WriteFile(filepath.Join(d, ".env.test"), []byte("FROM=test\n"), 0600)
	var sent map[string]any
	c := Client{BaseURL: snapshotServer(t, 4, &sent).URL}

	if n, e := Push(context.Background(), c, d, ".env.test", false, false); e != nil || n != 1 {
		t.Fatalf("pushed %d secrets: %v", n, e)
	}
	if got := sent["values"].(map[string]any)["FROM"]; got != "test" {
		t.Fatalf("pushed %v; the named file was ignored in favour of .env", got)
	}
	if _, e := Push(context.Background(), c, d, ".env.missing", false, false); e == nil || !strings.Contains(e.Error(), ".env.missing") {
		t.Fatalf("missing file reported as %v, expected it to name the file", e)
	}
}

// --clean exists so a pull-edit-push cycle leaves nothing plaintext behind.
func TestPushCleanRemovesTheFile(t *testing.T) {
	d := ctxDir(t)
	path := filepath.Join(d, ".env")
	_ = os.WriteFile(path, []byte("A=1\n"), 0600)
	var sent map[string]any
	c := Client{BaseURL: snapshotServer(t, 4, &sent).URL}

	if n, e := Push(context.Background(), c, d, "", false, true); e != nil || n != 1 {
		t.Fatalf("pushed %d: %v", n, e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(".env survived a --clean push")
	}
	// The revision still has to be recorded, or the next push is rejected.
	if x, _ := projectctx.Load(d); x.Environment.Revision != 5 {
		t.Fatalf("envi.toml revision is %d after --clean", x.Environment.Revision)
	}
}

// A failed push must leave the file alone: deleting it would destroy the only
// copy of work that never reached the server.
func TestPushCleanKeepsTheFileWhenThePushFails(t *testing.T) {
	d := ctxDir(t)
	path := filepath.Join(d, ".env")
	_ = os.WriteFile(path, []byte("A=1\n"), 0600)
	var sent map[string]any
	// The server holds revision 9; envi.toml says 4, so this is rejected.
	c := Client{BaseURL: snapshotServer(t, 9, &sent).URL}

	if _, e := Push(context.Background(), c, d, "", false, true); e == nil {
		t.Fatal("a stale push succeeded")
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatal("a failed --clean push deleted the file anyway")
	}
}

// Without the flag the file stays, which is the old behaviour.
func TestPushWithoutCleanKeepsTheFile(t *testing.T) {
	d := ctxDir(t)
	path := filepath.Join(d, ".env")
	_ = os.WriteFile(path, []byte("A=1\n"), 0600)
	var sent map[string]any
	c := Client{BaseURL: snapshotServer(t, 4, &sent).URL}

	if _, e := Push(context.Background(), c, d, "", false, false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatal("a plain push removed the file")
	}
}
