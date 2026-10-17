package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	projectctx "shellhaki/envi/internal/cli/project"
)

// Values chosen to break naive quoting in at least one of the four shells.
var awkward = []string{
	"plain",
	"it's",
	`back\slash`,
	"$HOME and `whoami` and $(id)",
	"line one\nline two",
	"curly ‘quotes’ too",
	"",
	`"double"`,
	"trailing backslash\\",
}

// Each shell's own parser has to read back exactly what was set.
func TestSetVarRoundTripsInRealShells(t *testing.T) {
	shells := map[string][]string{
		"bash": {"bash", "--norc", "--noprofile", "-c"},
		"zsh":  {"zsh", "-f", "-c"},
		"fish": {"fish", "--no-config", "-c"},
		"pwsh": {"pwsh", "-NoProfile", "-NonInteractive", "-Command"},
	}
	for shell, argv := range shells {
		if _, err := exec.LookPath(argv[0]); err != nil {
			t.Logf("%s not installed; skipped", shell)
			continue
		}
		for _, v := range awkward {
			script := setVar(shell, "ENVI_T", v) + "\n"
			switch shell {
			case "pwsh":
				script += "[Console]::Out.Write($env:ENVI_T)"
			case "fish":
				script += "printf '%s' \"$ENVI_T\""
			default:
				script += "printf '%s' \"$ENVI_T\""
			}
			out, err := exec.Command(argv[0], append(argv[1:], script)...).Output()
			if err != nil {
				t.Errorf("%s: %q: %v", shell, v, err)
				continue
			}
			got := string(out)
			if shell == "pwsh" && v == "" {
				// PowerShell cannot hold an empty environment variable; setting
				// one to '' removes it, which reads back as nothing all the same.
				got = ""
			}
			if got != v {
				t.Errorf("%s: set %q, read back %q", shell, v, got)
			}
		}
	}
}

// --- Export, driven the way a shell drives it --------------------------------

type fakeShell struct{ env map[string]string }

func (s *fakeShell) getenv(k string) string { return s.env[k] }

// apply runs the POSIX script Export printed against the fake environment.
// Export only ever prints `export K='v'` and `unset K` lines.
func (s *fakeShell) apply(t *testing.T, script string) {
	t.Helper()
	for _, line := range splitStatements(script) {
		switch {
		case strings.HasPrefix(line, "unset "):
			delete(s.env, strings.TrimPrefix(line, "unset "))
		case strings.HasPrefix(line, "export "):
			kv := strings.TrimPrefix(line, "export ")
			i := strings.IndexByte(kv, '=')
			s.env[kv[:i]] = unquotePOSIX(t, kv[i+1:])
		default:
			t.Fatalf("unexpected line %q", line)
		}
	}
}

// splitStatements splits on newlines that are outside single quotes, since a
// value may itself contain a newline.
func splitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range script {
		if r == '\'' {
			quoted = !quoted
		}
		if r == '\n' && !quoted {
			if cur.Len() > 0 {
				out = append(out, cur.String())
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func unquotePOSIX(t *testing.T, q string) string {
	t.Helper()
	if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
		t.Fatalf("not single-quoted: %q", q)
	}
	return strings.ReplaceAll(q[1:len(q)-1], `'\''`, "'")
}

type hookFixture struct {
	root, sub, outside string
	shell              *fakeShell
	trust              map[string]Allowed
	epoch              string
	secrets            map[string]string
	fetches            int
	fail               error
}

func newHookFixture(t *testing.T) *hookFixture {
	base := t.TempDir()
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	f := &hookFixture{
		root:    filepath.Join(base, "app"),
		sub:     filepath.Join(base, "app", "src", "deep"),
		outside: filepath.Join(base, "elsewhere"),
		shell:   &fakeShell{env: map[string]string{"NODE_ENV": "from-before"}},
		trust:   map[string]Allowed{},
		epoch:   "0",
		secrets: map[string]string{"DATABASE_URL": "postgres://x", "NODE_ENV": "development", "bad-name": "v"},
	}
	for _, d := range []string{f.sub, f.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := projectctx.Write(f.root, projectctx.Context{Version: 1,
		Project:     projectctx.Resource{ID: "p1", Name: "app"},
		Environment: projectctx.Resource{ID: "e1", Name: "development"}}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *hookFixture) allow(t *testing.T) {
	x, err := projectctx.Load(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.trust[f.root] = Allowed{Hash: trustKey(x)}
}

func (f *hookFixture) prompt(t *testing.T, cwd string) (string, []string) {
	t.Helper()
	script, notes := Export(context.Background(), "bash", cwd, exportDeps{
		getenv: f.shell.getenv, trust: f.trust, epoch: f.epoch,
		fetch: func(context.Context, string) (map[string]string, error) {
			f.fetches++
			return f.secrets, f.fail
		},
	})
	f.shell.apply(t, script)
	return script, notes
}

func TestExportNeedsAllowFirst(t *testing.T) {
	f := newHookFixture(t)
	_, notes := f.prompt(t, f.root)
	if f.fetches != 0 || f.shell.env["DATABASE_URL"] != "" {
		t.Fatal("loaded secrets for a folder that was never allowed")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "envi allow") {
		t.Errorf("notes = %q, want a pointer to envi allow", notes)
	}
	// Said once, not on every prompt.
	if script, notes := f.prompt(t, f.root); script != "" || len(notes) != 0 {
		t.Errorf("second prompt printed %q %q", script, notes)
	}
}

func TestExportLoadsStaysAndUnloads(t *testing.T) {
	f := newHookFixture(t)
	f.allow(t)

	_, notes := f.prompt(t, f.root)
	if f.shell.env["DATABASE_URL"] != "postgres://x" || f.shell.env["NODE_ENV"] != "development" {
		t.Fatalf("env after entering = %v", f.shell.env)
	}
	if _, ok := f.shell.env["bad-name"]; ok {
		t.Error("a name no shell can hold was exported")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "loaded 2 secrets") || !strings.Contains(notes[0], "1 skipped") {
		t.Errorf("notes = %q", notes)
	}

	// Moving around inside the project costs nothing.
	if script, _ := f.prompt(t, f.sub); script != "" || f.fetches != 1 {
		t.Errorf("subfolder: script %q after %d fetches", script, f.fetches)
	}

	_, notes = f.prompt(t, f.outside)
	if _, ok := f.shell.env["DATABASE_URL"]; ok {
		t.Error("DATABASE_URL survived leaving the project")
	}
	if f.shell.env["NODE_ENV"] != "from-before" {
		t.Errorf("NODE_ENV = %q, want the value from before the project overwrote it", f.shell.env["NODE_ENV"])
	}
	for k := range f.shell.env {
		if strings.HasPrefix(k, "ENVI_HOOK_") {
			t.Errorf("%s left behind", k)
		}
	}
	if len(notes) != 1 || notes[0] != "envi: unloaded" {
		t.Errorf("notes = %q", notes)
	}
}

func TestExportReloadsWhenAsked(t *testing.T) {
	f := newHookFixture(t)
	f.allow(t)
	f.prompt(t, f.root)
	f.secrets = map[string]string{"DATABASE_URL": "postgres://new"}
	f.epoch = "1"
	f.prompt(t, f.root)
	if f.shell.env["DATABASE_URL"] != "postgres://new" {
		t.Errorf("DATABASE_URL = %q after reload", f.shell.env["DATABASE_URL"])
	}
	if f.shell.env["NODE_ENV"] != "from-before" {
		t.Errorf("NODE_ENV = %q; a key gone from the server should restore the old value", f.shell.env["NODE_ENV"])
	}
}

func TestExportStopsWhenEnviTomlChanges(t *testing.T) {
	f := newHookFixture(t)
	f.allow(t)
	f.prompt(t, f.root)
	// Point the folder at another environment: the permission no longer applies.
	if err := projectctx.Write(f.root, projectctx.Context{Version: 1,
		Project:     projectctx.Resource{ID: "p1", Name: "app"},
		Environment: projectctx.Resource{ID: "e2", Name: "production"}}); err != nil {
		t.Fatal(err)
	}
	_, notes := f.prompt(t, f.root)
	if _, ok := f.shell.env["DATABASE_URL"]; ok {
		t.Error("secrets stayed loaded after envi.toml changed")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "envi allow") {
		t.Errorf("notes = %q", notes)
	}
}

func TestExportReportsFailureOnce(t *testing.T) {
	f := newHookFixture(t)
	f.allow(t)
	f.fail = errors.New("offline")
	_, notes := f.prompt(t, f.root)
	if len(notes) != 1 || !strings.Contains(notes[0], "offline") || !strings.Contains(notes[0], "envi reload") {
		t.Errorf("notes = %q", notes)
	}
	if script, notes := f.prompt(t, f.root); script != "" || len(notes) != 0 || f.fetches != 1 {
		t.Errorf("retried on its own: %q %q after %d fetches", script, notes, f.fetches)
	}
}

// --- installing into startup files -------------------------------------------

func TestInstallHookIsIdempotentAndReversible(t *testing.T) {
	home := os.Getenv("HOME")
	rc := filepath.Join(home, ".zshrc")
	original := "export PATH=\"$HOME/bin:$PATH\"\nalias ll='ls -l'"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(rc) })

	var out strings.Builder
	for i := 0; i < 2; i++ {
		if err := InstallHook("zsh", &out); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(rc)
	if n := strings.Count(string(b), hookBegin); n != 1 {
		t.Fatalf("hook present %d times after installing twice:\n%s", n, b)
	}
	if !strings.HasPrefix(string(b), original+"\n") {
		t.Errorf("existing lines disturbed:\n%s", b)
	}
	if !HookInstalled() {
		t.Error("HookInstalled = false right after installing")
	}

	if err := UninstallHook(&out); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(rc)
	if strings.TrimRight(string(b), "\n") != original {
		t.Errorf("after uninstall:\n%q\nwant\n%q", b, original)
	}
	if HookInstalled() {
		t.Error("HookInstalled = true after uninstalling")
	}
}

func TestInstallHookForEveryShell(t *testing.T) {
	var out strings.Builder
	for _, shell := range hookShells {
		if err := InstallHook(shell, &out); err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		f := rcFiles(shell)[0]
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), hookLine(shell)) {
			t.Errorf("%s: %s missing the hook line", shell, f)
		}
	}
	if err := UninstallHook(&out); err != nil {
		t.Fatal(err)
	}
	if HookInstalled() {
		t.Error("a hook survived uninstall")
	}
}

// --- the real thing ----------------------------------------------------------

// The built binary, in real shells, against a fake server: the hook loads on
// entering an allowed project and unloads on leaving.
func TestHookEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI")
	}
	bin := filepath.Join(t.TempDir(), "envi")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "shellhaki/envi/cmd/envi")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/environments/e1/secrets/snapshot" || r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"no"}`, 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"values": map[string]string{"GREETING": "it's alive"}, "revision": 1})
	}))
	defer server.Close()

	f := newHookFixture(t)
	config := filepath.Join(t.TempDir(), "envi")
	t.Setenv("ENVI_CONFIG_DIR", config)
	x, _ := projectctx.Load(f.root)
	hash := trustKey(x)
	if err := (trustStore{path: filepath.Join(config, "allowed.json")}).save(map[string]Allowed{f.root: {Hash: hash, AllowedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "ENVI_API_URL="+server.URL, "ENVI_TOKEN=test-token", "ENVI_CONFIG_DIR="+config, "NO_COLOR=1")

	shells := map[string][]string{
		"bash": {"bash", "--norc", "--noprofile", "-c", `eval "$("$ENVI" hook bash)"; cd "$IN"; _envi_hook; echo "in:$GREETING"; cd "$OUT"; _envi_hook; echo "out:${GREETING-unset}"`},
		"zsh":  {"zsh", "-f", "-c", `eval "$("$ENVI" hook zsh)"; cd "$IN"; _envi_hook; echo "in:$GREETING"; cd "$OUT"; _envi_hook; echo "out:${GREETING-unset}"`},
		"pwsh": {"pwsh", "-NoProfile", "-NonInteractive", "-Command", `function global:prompt { "PS> " }; Invoke-Expression ((& $env:ENVI hook pwsh) -join "` + "`n" + `"); Set-Location $env:IN; $null = prompt; Write-Output "in:$env:GREETING"; Set-Location $env:OUT; $null = prompt; Write-Output ("out:" + $(if ($env:GREETING) { $env:GREETING } else { "unset" }))`},
	}
	for shell, argv := range shells {
		if _, err := exec.LookPath(argv[0]); err != nil {
			t.Logf("%s not installed; skipped", shell)
			continue
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = append(env, "ENVI="+bin, "IN="+f.sub, "OUT="+f.outside)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s", shell, err, out)
			continue
		}
		got := string(out)
		if !strings.Contains(got, "in:it's alive") || !strings.Contains(got, "out:unset") {
			t.Errorf("%s output:\n%s", shell, got)
		}
		if !strings.Contains(got, "envi: loaded 1 secret") || !strings.Contains(got, "envi: unloaded") {
			t.Errorf("%s: notices missing:\n%s", shell, got)
		}
	}
}

// A push or pull rewrites envi.toml with the new revision. That must keep the
// permission and reload the secrets, not take the permission away.
func TestExportSurvivesRevisionChange(t *testing.T) {
	f := newHookFixture(t)
	f.allow(t)
	f.prompt(t, f.root)
	if err := projectctx.Write(f.root, projectctx.Context{Version: 1,
		Project:     projectctx.Resource{ID: "p1", Name: "app"},
		Environment: projectctx.Resource{ID: "e1", Name: "development", Revision: 7}}); err != nil {
		t.Fatal(err)
	}
	f.secrets = map[string]string{"DATABASE_URL": "postgres://pushed"}
	_, notes := f.prompt(t, f.root)
	if f.shell.env["DATABASE_URL"] != "postgres://pushed" {
		t.Errorf("DATABASE_URL = %q after a push here", f.shell.env["DATABASE_URL"])
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "loaded 1 secret") {
		t.Errorf("notes = %q", notes)
	}
}
