package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeWrangler puts a wrangler on PATH that records each call (its arguments, then its stdin) in a
// log, and answers `secret list` with the given names. With gradual set, `secret bulk` is refused the
// way wrangler refuses it for a Worker on a gradual deployment.
func fakeWrangler(t *testing.T, onWorker []string, gradual bool) (dir, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	bin := t.TempDir()
	log = filepath.Join(bin, "calls.log")
	list := "["
	for i, n := range onWorker {
		if i > 0 {
			list += ","
		}
		list += fmt.Sprintf(`{"name":%q,"type":"secret_text"}`, n)
	}
	list += "]"
	refuse := ""
	if gradual {
		refuse = `if [ "$1 $2" = "secret bulk" ]; then cat >/dev/null; echo "Secret edit failed. The latest version of your Worker isn't currently deployed. Use wrangler versions secret bulk"; exit 1; fi`
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "ARGS $*" >> %q
%s
case "$1" in
  whoami) echo "You are logged in";;
  secret) if [ "$2" = list ]; then echo 'banner'; echo '%s'; elif [ "$2" = bulk ]; then cat >> %q; echo >> %q; fi;;
  versions) cat >> %q; echo >> %q;;
  dev|deploy) env > %q;;
esac
`, log, refuse, list, log, log, log, log, filepath.Join(bin, "env.txt"))
	if err := os.WriteFile(filepath.Join(bin, "wrangler"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir = t.TempDir()
	config := `name = "api"
[vars]
PUBLIC_URL = "https://example.com"
[env.staging]
name = "api-staging"
[env.staging.vars]
MODE = "staging"
`
	if err := os.WriteFile(filepath.Join(dir, "wrangler.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, log
}

func readLog(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// bulkBodies returns the JSON each bulk call received on stdin.
func bulkBodies(t *testing.T, log string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(readLog(t, log), "\n") {
		if strings.HasPrefix(line, "{") {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("bulk body is not JSON: %q", line)
			}
			out = append(out, m)
		}
	}
	return out
}

func mustWorker(t *testing.T, dir string, f workersFlags) worker {
	t.Helper()
	w, err := workerFor(dir, f)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Values go on stdin, never in an argument; [vars] names are left out.
func TestWorkersPushSendsValuesOnStdin(t *testing.T) {
	dir, log := fakeWrangler(t, []string{"API_KEY"}, false)
	src := workerSource{values: map[string]string{"API_KEY": "sk-live-1", "DB_URL": "postgres://x", "PUBLIC_URL": "y"}, label: "demo · development", origin: "development"}
	var out bytes.Buffer
	n, err := WorkersPush(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &out, src, workersFlags{})
	if err != nil {
		t.Fatal(err, out.String())
	}
	if n != 2 {
		t.Fatalf("sent %d, want 2", n)
	}
	calls := readLog(t, log)
	if strings.Contains(strings.Join(argLines(calls), "\n"), "sk-live-1") {
		t.Fatal("a value was passed as an argument")
	}
	bodies := bulkBodies(t, log)
	if len(bodies) != 1 || bodies[0]["API_KEY"] != "sk-live-1" || bodies[0]["DB_URL"] != "postgres://x" || bodies[0]["PUBLIC_URL"] != nil {
		t.Fatalf("bulk bodies = %v", bodies)
	}
	if !strings.Contains(out.String(), "PUBLIC_URL") || !strings.Contains(out.String(), "add 1: DB_URL") || !strings.Contains(out.String(), "update 1: API_KEY") {
		t.Fatalf("output = %q", out.String())
	}
}

func argLines(log string) []string {
	var out []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "ARGS ") {
			out = append(out, l)
		}
	}
	return out
}

func TestWorkersPushBatchesAtOneHundred(t *testing.T) {
	dir, log := fakeWrangler(t, nil, false)
	values := map[string]string{}
	for i := range 250 {
		values[fmt.Sprintf("K%03d", i)] = "v"
	}
	src := workerSource{values: values, label: "demo · development", origin: "development"}
	if _, err := WorkersPush(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &bytes.Buffer{}, src, workersFlags{}); err != nil {
		t.Fatal(err)
	}
	bodies := bulkBodies(t, log)
	if len(bodies) != 3 || len(bodies[0]) != 100 || len(bodies[2]) != 50 {
		t.Fatalf("batches = %d", len(bodies))
	}
}

func TestWorkersPushPruneOnlyAndDryRun(t *testing.T) {
	dir, log := fakeWrangler(t, []string{"OLD", "KEEP_ME", "STRIPE_KEY"}, false)
	src := workerSource{values: map[string]string{"STRIPE_KEY": "a", "STRIPE_HOOK": "b", "OTHER": "c"}, label: "demo · development", origin: "development"}

	var out bytes.Buffer
	f := workersFlags{prune: true, only: []string{"STRIPE_*"}, dryRun: true}
	if _, err := WorkersPush(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &out, src, f); err != nil {
		t.Fatal(err)
	}
	if len(bulkBodies(t, log)) != 0 || !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("dry run sent something: %q", out.String())
	}

	f.dryRun = false
	f.only = nil
	f.except = []string{"KEEP_ME"}
	if _, err := WorkersPush(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &bytes.Buffer{}, src, f); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if !strings.Contains(calls, "ARGS secret delete OLD") {
		t.Fatalf("OLD not deleted: %s", calls)
	}
	if strings.Contains(calls, "delete KEEP_ME") || strings.Contains(calls, "delete STRIPE_KEY") {
		t.Fatal("deleted a name it should have kept")
	}
	for _, b := range bulkBodies(t, log) {
		for k, v := range b {
			if _, ok := v.(string); !ok {
				t.Fatalf("%s sent as %v; wrangler only takes strings", k, v)
			}
		}
	}
}

func TestWorkersPushAsksForProduction(t *testing.T) {
	dir, log := fakeWrangler(t, nil, false)
	src := workerSource{values: map[string]string{"A": "1"}, label: "demo · production", origin: "production", production: true}
	w := mustWorker(t, dir, workersFlags{})
	if _, err := WorkersPush(context.Background(), w, strings.NewReader(""), &bytes.Buffer{}, src, workersFlags{}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if len(bulkBodies(t, log)) != 0 {
		t.Fatal("sent without confirmation")
	}
	if _, err := WorkersPush(context.Background(), w, strings.NewReader("y\n"), &bytes.Buffer{}, src, workersFlags{}); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkersPush(context.Background(), w, nil, &bytes.Buffer{}, src, workersFlags{yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(bulkBodies(t, log)) != 2 {
		t.Fatal("confirmed pushes were not sent")
	}
}

func TestWorkersPushToWranglerEnvironment(t *testing.T) {
	dir, log := fakeWrangler(t, nil, false)
	src := workerSource{values: map[string]string{"MODE": "x", "B": "2"}, origin: "staging", mapping: map[string]string{"staging": "staging"}}
	env, ok := wranglerEnv(workersFlags{}, src)
	if !ok || env != "staging" {
		t.Fatalf("mapping gave %q %v", env, ok)
	}
	if _, ok := wranglerEnv(workersFlags{envSet: true}, src); ok {
		t.Fatal("--env should win over the mapping")
	}
	w := mustWorker(t, dir, workersFlags{env: env})
	if w.wrangler.Target() != "api-staging (staging)" {
		t.Fatalf("target = %q", w.wrangler.Target())
	}
	if _, err := WorkersPush(context.Background(), w, nil, &bytes.Buffer{}, src, workersFlags{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(t, log), "ARGS secret bulk --env staging") {
		t.Fatalf("calls = %s", readLog(t, log))
	}
	// MODE is a var only in [env.staging], so only that environment skips it.
	if b := bulkBodies(t, log)[0]; b["MODE"] != nil || b["B"] != "2" {
		t.Fatalf("body = %v", b)
	}
}

func TestWorkersPushFallsBackToVersionsForGradualDeployments(t *testing.T) {
	dir, log := fakeWrangler(t, nil, true)
	src := workerSource{values: map[string]string{"A": "1"}, origin: "development"}
	var out bytes.Buffer
	if _, err := WorkersPush(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &out, src, workersFlags{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(t, log), "ARGS versions secret bulk") || !strings.Contains(out.String(), "versions deploy") {
		t.Fatalf("calls = %s\nout = %s", readLog(t, log), out.String())
	}
}

func TestWorkersDevInjectsOnlySecretsAndWhatWranglerNeeds(t *testing.T) {
	dir, _ := fakeWrangler(t, nil, false)
	t.Setenv("UNRELATED_SHELL_VAR", "leak")
	src := workerSource{values: map[string]string{"API_KEY": "sk-1"}, label: "demo · development"}
	w := mustWorker(t, dir, workersFlags{})
	status, err := WorkersDev(w, &bytes.Buffer{}, src, workersFlags{rest: []string{"--port", "8788"}})
	if err != nil || status != 0 {
		t.Fatal(status, err)
	}
	env, _ := os.ReadFile(filepath.Join(filepath.Dir(w.wrangler.bin), "env.txt"))
	got := string(env)
	if !strings.Contains(got, "API_KEY=sk-1") || !strings.Contains(got, "CLOUDFLARE_INCLUDE_PROCESS_ENV=true") || !strings.Contains(got, "PATH=") {
		t.Fatalf("env = %s", got)
	}
	if strings.Contains(got, "UNRELATED_SHELL_VAR") {
		t.Fatal("the shell's environment leaked into wrangler dev")
	}
}

func TestWorkersStatusComparesNames(t *testing.T) {
	dir, _ := fakeWrangler(t, []string{"A", "EXTRA"}, false)
	src := workerSource{values: map[string]string{"A": "1", "B": "2"}, label: "demo · development", origin: "development"}
	var out bytes.Buffer
	if err := WorkersStatus(context.Background(), mustWorker(t, dir, workersFlags{}), &out, src, workersFlags{}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "1 on both") || !strings.Contains(s, "missing on the Worker 1: B") || !strings.Contains(s, "only on the Worker 1: EXTRA") {
		t.Fatalf("status = %q", s)
	}
}

func TestWorkersDeleteOneKey(t *testing.T) {
	dir, log := fakeWrangler(t, nil, false)
	w := mustWorker(t, dir, workersFlags{})
	if err := WorkersDelete(context.Background(), w.wrangler, nil, &bytes.Buffer{}, "OLD", true); err != nil {
		t.Fatal(err)
	}
	if calls := readLog(t, log); !strings.Contains(calls, "ARGS secret delete OLD") || len(bulkBodies(t, log)) != 0 {
		t.Fatalf("calls = %s", calls)
	}
}

func TestReadWorkerConfigJSONC(t *testing.T) {
	dir := t.TempDir()
	jsonc := `{
  // the Worker
  "name": "api", /* inline */
  "vars": { "URL": "https://a//b", },
  "env": { "production": { "vars": { "LEVEL": "warn", }, // trailing
  } },
}`
	if err := os.WriteFile(filepath.Join(dir, "wrangler.jsonc"), []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	top, err := readWorkerConfig(dir, "", "")
	if err != nil || top.Name != "api" || !top.Vars["URL"] || top.Vars["LEVEL"] {
		t.Fatalf("top = %+v, %v", top, err)
	}
	prod, err := readWorkerConfig(dir, "", "production")
	if err != nil || prod.Name != "api-production" || !prod.Vars["LEVEL"] || prod.Vars["URL"] {
		t.Fatalf("production = %+v, %v", prod, err)
	}
}

func TestParseWorkersArgs(t *testing.T) {
	f, err := parseWorkersArgs("delete", []string{"OLD", "--env", "staging", "--yes"})
	if err != nil || len(f.rest) != 1 || f.rest[0] != "OLD" || f.env != "staging" || !f.envSet || !f.yes {
		t.Fatalf("%+v %v", f, err)
	}
	f, err = parseWorkersArgs("dev", []string{"--only", "A, B", "--", "--port", "8788"})
	if err != nil || strings.Join(f.only, "|") != "A|B" || strings.Join(f.rest, " ") != "--port 8788" || f.envSet {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestWorkersDeployPushesThenDeploys(t *testing.T) {
	dir, log := fakeWrangler(t, nil, false)
	src := workerSource{values: map[string]string{"A": "1"}, origin: "development"}
	status, err := WorkersDeploy(context.Background(), mustWorker(t, dir, workersFlags{}), nil, &bytes.Buffer{}, src, workersFlags{rest: []string{"--minify"}})
	if err != nil || status != 0 {
		t.Fatal(status, err)
	}
	calls := argLines(readLog(t, log))
	last := calls[len(calls)-1]
	if last != "ARGS deploy --minify" || !strings.Contains(strings.Join(calls, "\n"), "ARGS secret bulk") {
		t.Fatalf("calls = %v", calls)
	}
}
