package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	projectctx "shellhaki/envi/internal/cli/project"
)

// runServer answers the snapshot and origin-listing calls run makes. Each
// environment id maps to the values that environment holds.
func runServer(t *testing.T, envs map[string]map[string]string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/environments") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "e-dev", "name": "development", "is_production": false},
				{"id": "e-prod", "name": "production", "is_production": true},
			})
			return
		}
		for id, values := range envs {
			if strings.Contains(r.URL.Path, "/environments/"+id+"/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"values": values, "revision": 1})
				return
			}
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(s.Close)
	return s
}

func runDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := projectctx.Write(d, projectctx.Context{Version: 1,
		Project:     projectctx.Resource{ID: "p", Name: "demo"},
		Environment: projectctx.Resource{ID: "e-dev", Name: "development", Revision: 1},
	}); e != nil {
		t.Fatal(e)
	}
	return d
}

// The whole point of the command: the child sees the secrets, and nothing is
// written to disk on the way there.
func TestRunInjectsSecretsWithoutTouchingDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := runDir(t)
	srv := runServer(t, map[string]map[string]string{"e-dev": {"API_KEY": "sk-live-1", "PORT": "3000"}})

	stdout := captureStdout(t)
	var errOut bytes.Buffer
	code, err := Run(context.Background(), Client{BaseURL: srv.URL}, dir, "", false,
		[]string{"sh", "-c", "printf '%s|%s' \"$API_KEY\" \"$PORT\""}, &errOut, nil)
	got := stdout()

	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q", code, err, errOut.String())
	}
	if got != "sk-live-1|3000" {
		t.Fatalf("child saw %q, want the injected values", got)
	}
	if _, e := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(e) {
		t.Fatal("run wrote a .env; the command exists precisely so it does not")
	}
}

func TestRunExitStatusIsTheChilds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := runDir(t)
	srv := runServer(t, map[string]map[string]string{"e-dev": {"A": "1"}})
	c := Client{BaseURL: srv.URL}

	for _, want := range []int{0, 3, 42} {
		var errOut bytes.Buffer
		code, err := Run(context.Background(), c, dir, "", false,
			[]string{"sh", "-c", fmt.Sprintf("exit %d", want)}, &errOut, nil)
		if err != nil {
			t.Fatalf("exit %d: %v", want, err)
		}
		if code != want {
			t.Fatalf("child exited %d, run reported %d", want, code)
		}
	}

	var errOut bytes.Buffer
	_, err := Run(context.Background(), c, dir, "", false,
		[]string{"definitely-not-a-real-binary-xyz"}, &errOut, nil)
	if err == nil || !strings.Contains(err.Error(), "command not found") {
		t.Fatalf("got %v, want a command-not-found error", err)
	}
	// Shells report 127 for this, and anything wrapping envi run is really
	// wrapping the command.
	var ee *ExecError
	if !errors.As(err, &ee) || ee.Status != 127 {
		t.Fatalf("missing binary reported status %v, want 127", err)
	}
}

func TestRunOrigin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := runDir(t)
	srv := runServer(t, map[string]map[string]string{
		"e-dev":  {"TARGET": "dev"},
		"e-prod": {"TARGET": "prod"},
	})
	c := Client{BaseURL: srv.URL}

	stdout := captureStdout(t)
	var errOut bytes.Buffer
	if _, err := Run(context.Background(), c, dir, "production", false,
		[]string{"sh", "-c", "printf %s \"$TARGET\""}, &errOut, nil); err != nil {
		t.Fatal(err)
	}
	if got := stdout(); got != "prod" {
		t.Fatalf("--origin production gave the child %q", got)
	}

	// envi.toml must be untouched: --origin is for one command, not a switch.
	if x, _ := projectctx.Load(dir); x.Environment.ID != "e-dev" {
		t.Fatalf("--origin repointed envi.toml at %s", x.Environment.ID)
	}

	_, err := Run(context.Background(), c, dir, "staging", false, []string{"true"}, &errOut, nil)
	if err == nil || !strings.Contains(err.Error(), "development, production") {
		t.Fatalf("got %v, want the available origins listed", err)
	}
}

func TestRunRequiresAProjectAndACommand(t *testing.T) {
	srv := runServer(t, map[string]map[string]string{"e-dev": {}})
	c := Client{BaseURL: srv.URL}
	var errOut bytes.Buffer

	if _, err := Run(context.Background(), c, runDir(t), "", false, nil, &errOut, nil); err == nil ||
		!strings.Contains(err.Error(), "no command") {
		t.Fatalf("got %v, want a missing-command error", err)
	}
	if _, err := Run(context.Background(), c, t.TempDir(), "", false, []string{"true"}, &errOut, nil); err == nil {
		t.Fatal("ran outside an initialized directory")
	}
}

func TestComposeEnvPrecedence(t *testing.T) {
	parent := []string{"PATH=/usr/bin", "PORT=9999"}
	secrets := map[string]string{"PORT": "3000", "API_KEY": "sk-1"}

	env, injected, skipped := composeEnv(parent, secrets, false)
	if skipped != nil {
		t.Fatalf("skipped %v", skipped)
	}
	if injected != 2 {
		t.Fatalf("injected %d, want 2", injected)
	}
	if v := lookup(env, "PORT"); v != "3000" {
		t.Fatalf("PORT=%q; the secret should win by default", v)
	}
	if v := lookup(env, "PATH"); v != "/usr/bin" {
		t.Fatalf("PATH=%q; names Envi knows nothing about must survive", v)
	}
	// One entry per name, so a child reading its own environ directly cannot
	// see a shadowed duplicate.
	if n := count(env, "PORT"); n != 1 {
		t.Fatalf("PORT appears %d times in the environment block", n)
	}

	env, injected, _ = composeEnv(parent, secrets, true)
	if v := lookup(env, "PORT"); v != "9999" {
		t.Fatalf("PORT=%q with --preserve-env; the inherited value should win", v)
	}
	if v := lookup(env, "API_KEY"); v != "sk-1" {
		t.Fatalf("API_KEY=%q; --preserve-env must still add names the parent lacks", v)
	}
	if injected != 1 {
		t.Fatalf("injected %d with --preserve-env, want 1", injected)
	}
}

// Key names are not validated on write, so a name that cannot survive an
// environment block has to be dropped rather than silently renaming a variable.
func TestComposeEnvSkipsUnusableNames(t *testing.T) {
	env, injected, skipped := composeEnv(nil, map[string]string{
		"GOOD": "1", "BAD=NAME": "2", "NUL\x00": "3", "": "4",
	}, false)
	if !reflect.DeepEqual(skipped, []string{"", "BAD=NAME", "NUL\x00"}) {
		t.Fatalf("skipped %q", skipped)
	}
	if injected != 1 || len(env) != 1 || env[0] != "GOOD=1" {
		t.Fatalf("env=%q injected=%d", env, injected)
	}
}

func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		args     []string
		origin   string
		preserve bool
		argv     []string
	}{
		{[]string{"--", "npm", "start"}, "", false, []string{"npm", "start"}},
		{[]string{"npm", "start"}, "", false, []string{"npm", "start"}},
		{[]string{"--origin", "prod", "--", "./deploy.sh"}, "prod", false, []string{"./deploy.sh"}},
		{[]string{"--preserve-env", "--", "npm", "start"}, "", true, []string{"npm", "start"}},
		// Flags belonging to the child must reach it, including a second --.
		{[]string{"--", "npm", "run", "dev", "--", "--port", "3000"}, "", false,
			[]string{"npm", "run", "dev", "--", "--port", "3000"}},
		{[]string{"--", "ls", "--origin"}, "", false, []string{"ls", "--origin"}},
		{[]string{}, "", false, []string{}},
	}
	for _, c := range cases {
		origin, preserve, argv, err := parseRunArgs(c.args)
		if err != nil {
			t.Errorf("run %v: %v", c.args, err)
			continue
		}
		if origin != c.origin || preserve != c.preserve || !reflect.DeepEqual(argv, c.argv) {
			t.Errorf("run %v -> origin=%q preserve=%v argv=%v, want origin=%q preserve=%v argv=%v",
				c.args, origin, preserve, argv, c.origin, c.preserve, c.argv)
		}
	}
	if _, _, _, err := parseRunArgs([]string{"--nonsense", "--", "ls"}); err == nil {
		t.Error("an unknown envi flag was accepted")
	}
}

// captureStdout redirects the real os.Stdout, which is what run hands the child.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	var out string
	var read bool
	t.Cleanup(func() {
		if !read {
			os.Stdout = saved
			_ = w.Close()
		}
	})
	return func() string {
		if !read {
			read = true
			os.Stdout = saved
			_ = w.Close()
			out = <-done
		}
		return out
	}
}

func lookup(env []string, name string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

func count(env []string, name string) int {
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, name+"=") {
			n++
		}
	}
	return n
}
