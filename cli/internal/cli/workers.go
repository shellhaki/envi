package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const workersUsage = `usage: envi workers <command> [flags]

Commands:
  push              Send this origin's secrets to the Worker (wrangler secret bulk)
  status            Compare the names in Envi with the Worker's secrets
  delete <key>      Delete one secret from the Worker
  dev [-- args]     Run wrangler dev with the secrets, nothing written to .dev.vars
  deploy [-- args]  Push, then wrangler deploy

Flags:
  --origin <name>   Envi origin to read (default: the linked one)
  --env <name>      wrangler environment (default: [workers] in envi.toml, else the top-level Worker)
  --name <worker>   Worker name, when it isn't the one in the wrangler config
  -c <path>         wrangler config file
  --only <keys>     Only these keys (comma separated, * matches)
  --except <keys>   Every key but these
  --prune           Also delete Worker secrets that aren't in Envi (push, deploy)
  --dry-run         Show what would change, names only (push, deploy)
  --yes             Don't ask before writing to a production origin's Worker`

// workersFlags are the flags every workers command shares.
type workersFlags struct {
	origin, env, name, config string
	envSet                    bool
	only, except              []string
	prune, dryRun, yes        bool
	rest                      []string // positional arguments, and anything after --
}

func parseWorkersArgs(command string, args []string) (workersFlags, error) {
	var f workersFlags
	var passthrough []string
	for i, a := range args {
		if a == "--" {
			args, passthrough = args[:i], args[i+1:]
			break
		}
	}
	fs := flag.NewFlagSet("workers "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.origin, "origin", "", "")
	fs.StringVar(&f.env, "env", "", "")
	fs.StringVar(&f.env, "e", "", "")
	fs.StringVar(&f.name, "name", "", "")
	fs.StringVar(&f.config, "c", "", "")
	fs.StringVar(&f.config, "config", "", "")
	only := fs.String("only", "", "")
	except := fs.String("except", "", "")
	fs.BoolVar(&f.prune, "prune", false, "")
	fs.BoolVar(&f.dryRun, "dry-run", false, "")
	fs.BoolVar(&f.yes, "yes", false, "")
	fs.BoolVar(&f.yes, "y", false, "")

	// Flags may come after positionals (envi workers delete KEY --env staging).
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return f, err
		}
		args = fs.Args()
		if len(args) > 0 {
			f.rest = append(f.rest, args[0])
			args = args[1:]
		}
	}
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "env" || fl.Name == "e" {
			f.envSet = true
		}
	})
	f.only, f.except = splitKeys(*only), splitKeys(*except)
	f.rest = append(f.rest, passthrough...)
	return f, nil
}

func splitKeys(s string) []string {
	var out []string
	for k := range strings.SplitSeq(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// matches reports whether key matches one of the patterns (exact, or with * as a wildcard).
func matches(key string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, key); ok || p == key {
			return true
		}
	}
	return false
}

// workers runs `envi workers ...`.
func (a App) workers(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.Out, workersUsage)
		if len(args) == 0 {
			return ExitUsage
		}
		return ExitOK
	}
	command := args[0]
	f, err := parseWorkersArgs(command, args[1:])
	if err != nil {
		fmt.Fprintln(a.Err, err)
		fmt.Fprintln(a.Err, workersUsage)
		return ExitUsage
	}
	switch command {
	case "push", "status", "dev", "deploy":
		if command != "dev" && command != "deploy" && len(f.rest) > 0 {
			fmt.Fprintf(a.Err, "envi workers %s takes no arguments\n", command)
			return ExitUsage
		}
	case "delete":
		if len(f.rest) != 1 {
			fmt.Fprintln(a.Err, "usage: envi workers delete <key> [--env <name>] [--yes]")
			return ExitUsage
		}
	default:
		fmt.Fprintf(a.Err, "unknown workers command %q\n%s\n", command, workersUsage)
		return ExitUsage
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitConfig
	}
	ctx := context.Background()
	w, err := workerFor(dir, f)
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitConfig
	}

	// delete touches only the Worker, so it needs no Envi session.
	if command == "delete" {
		return a.workersResult(WorkersDelete(ctx, w.wrangler, a.input(), a.Out, f.rest[0], f.yes))
	}

	c, code := a.session()
	if code != ExitOK {
		return code
	}
	fetching := NewUI(a.Out).Spinner("Fetching secrets")
	src, err := workerSecrets(ctx, c, dir, f.origin)
	fetching.Stop()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitCode(err)
	}
	// The origin decides the wrangler environment unless --env was given.
	if env, ok := wranglerEnv(f, src); ok {
		if w, err = workerFor(dir, workersFlags{env: env, name: f.name, config: f.config}); err != nil {
			fmt.Fprintln(a.Err, err)
			return ExitConfig
		}
	}

	switch command {
	case "status":
		return a.workersResult(WorkersStatus(ctx, w, a.Out, src, f))
	case "push":
		_, err := WorkersPush(ctx, w, a.input(), a.Out, src, f)
		return a.workersResult(err)
	case "dev":
		status, err := WorkersDev(w, a.Err, src, f)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return ExitAPI
		}
		return status
	default: // deploy
		status, err := WorkersDeploy(ctx, w, a.input(), a.Out, src, f)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return ExitAPI
		}
		return status
	}
}

// wranglerEnv is the wrangler environment [workers] in envi.toml gives the origin, unless --env was passed.
func wranglerEnv(f workersFlags, src workerSource) (string, bool) {
	if f.envSet {
		return "", false
	}
	env, ok := src.mapping[src.origin]
	return env, ok
}

func (a App) workersResult(err error) int {
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitAPI
	}
	return ExitOK
}

// A worker is the Worker the secrets go to: the wrangler that reaches it and its config.
type worker struct {
	wrangler *wrangler
	config   workerConfig
}

func workerFor(dir string, f workersFlags) (worker, error) {
	bin, err := findWrangler(dir)
	if err != nil {
		return worker{}, err
	}
	cfg, err := readWorkerConfig(dir, f.config, f.env)
	if err != nil && cfg.Path == "" && f.name == "" {
		return worker{}, err
	}
	name := f.name
	if name == "" {
		name = cfg.Name
	}
	if name == "" {
		name = "the Worker"
	}
	return worker{config: cfg, wrangler: &wrangler{bin: bin, dir: dir, env: f.env, name: f.name, config: f.config, worker: name}}, nil
}

// workerSource is what an origin holds, and what envi knows about it.
type workerSource struct {
	values     map[string]string
	label      string // "project · origin", for messages
	origin     string
	production bool
	mapping    map[string]string // [workers] in envi.toml
}

func workerSecrets(ctx context.Context, c Client, dir, originName string) (workerSource, error) {
	x, err := loadContext(dir)
	if err != nil {
		// No envi.toml: a service token in CI, named by ENVI_PROJECT and ENVI_ENVIRONMENT.
		values, label, err := secretsToInject(ctx, c, dir, originName)
		if err != nil {
			return workerSource{}, err
		}
		name := originName
		if name == "" {
			name = strings.TrimSpace(os.Getenv("ENVI_ENVIRONMENT"))
		}
		return workerSource{values: values, label: label, origin: name}, nil
	}
	name := x.Environment.Name
	if originName != "" {
		name = originName
	}
	origins, err := listOrigins(ctx, c, x.Project.ID)
	if err != nil {
		return workerSource{}, err
	}
	target, err := findOrigin(origins, name)
	if err != nil {
		return workerSource{}, err
	}
	values, _, err := fetchSnapshot(ctx, c, target.ID)
	if err != nil {
		return workerSource{}, err
	}
	return workerSource{values: values, label: x.Project.Name + " · " + target.Name, origin: target.Name, production: target.Production, mapping: x.Workers}, nil
}

// selected applies --only and --except, then leaves out the names that are [vars] in the wrangler
// config: a Worker can't have a var and a secret with one name. It returns what it left out for vars.
func selected(src workerSource, f workersFlags, cfg workerConfig) (map[string]string, []string) {
	out := map[string]string{}
	var vars []string
	for k, v := range src.values {
		if len(f.only) > 0 && !matches(k, f.only) {
			continue
		}
		if matches(k, f.except) {
			continue
		}
		if cfg.Vars[k] {
			vars = append(vars, k)
			continue
		}
		out[k] = v
	}
	sort.Strings(vars)
	return out, vars
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WorkersStatus compares names only: Cloudflare never returns a secret's value.
func WorkersStatus(ctx context.Context, w worker, out io.Writer, src workerSource, f workersFlags) error {
	if err := w.wrangler.signedIn(ctx); err != nil {
		return err
	}
	values, vars := selected(src, f, w.config)
	remote, err := w.wrangler.List(ctx)
	if err != nil {
		return err
	}
	onWorker := map[string]bool{}
	for _, k := range remote {
		onWorker[k] = true
	}
	var both, missing, extra []string
	for _, k := range sortedKeys(values) {
		if onWorker[k] {
			both = append(both, k)
		} else {
			missing = append(missing, k)
		}
	}
	for _, k := range remote {
		if _, ok := values[k]; !ok && !matches(k, f.except) && (len(f.only) == 0 || matches(k, f.only)) {
			extra = append(extra, k)
		}
	}

	ui := NewUI(out)
	ui.Print("%s  →  %s", src.label, ui.Bold(w.wrangler.Target()))
	ui.Print("  %d on both", len(both))
	report(out, ui, "missing on the Worker", missing)
	report(out, ui, "only on the Worker", extra)
	for _, k := range vars {
		ui.Warn("%s is in [vars] in %s, so it isn't sent as a secret.", k, baseName(w.config.Path))
	}
	if len(missing) == 0 && len(extra) == 0 {
		ui.Success("The Worker has every secret in %s", src.origin)
	} else {
		ui.Print(ui.Dim("Run envi workers push to send them" + map[bool]string{true: " (add --prune to delete the extra ones)", false: ""}[len(extra) > 0] + "."))
	}
	return nil
}

func baseName(p string) string {
	if p == "" {
		return "the wrangler config"
	}
	return p[strings.LastIndexAny(p, `/\`)+1:]
}

// WorkersPush sends the origin's secrets to the Worker and reports how many it sent.
func WorkersPush(ctx context.Context, w worker, in io.Reader, out io.Writer, src workerSource, f workersFlags) (int, error) {
	if err := w.wrangler.signedIn(ctx); err != nil {
		return 0, err
	}
	values, vars := selected(src, f, w.config)
	ui := NewUI(out)
	for _, k := range vars {
		ui.Warn("Skipped %s: it is in [vars] in %s, and a Worker can't have a var and a secret with one name.", k, baseName(w.config.Path))
	}

	// The Worker's current names show what's new; a brand-new Worker has none yet.
	remote, err := w.wrangler.List(ctx)
	if err != nil && f.prune {
		return 0, err
	}
	onWorker := map[string]bool{}
	for _, k := range remote {
		onWorker[k] = true
	}
	var added, updated, removed []string
	for _, k := range sortedKeys(values) {
		if onWorker[k] {
			updated = append(updated, k)
		} else {
			added = append(added, k)
		}
	}
	if f.prune {
		for _, k := range remote {
			if _, ok := values[k]; !ok && !matches(k, f.except) && (len(f.only) == 0 || matches(k, f.only)) {
				removed = append(removed, k)
			}
		}
	}
	if len(values)+len(removed) == 0 {
		ui.Success("Nothing to send from %s", src.label)
		return 0, nil
	}

	ui.Print("%s  →  %s", src.label, ui.Bold(w.wrangler.Target()))
	report(out, ui, "add", added)
	report(out, ui, "update", updated)
	report(out, ui, "delete", removed)
	if f.dryRun {
		ui.Print(ui.Dim("Dry run: nothing was sent."))
		return 0, nil
	}
	if src.production && !f.yes {
		ui.Warn("%s is a production origin.", src.origin)
		if !confirmWith(ui, in, fmt.Sprintf("Send to %s?", w.wrangler.Target())) {
			return 0, errors.New("not sent; pass --yes to send a production origin without asking")
		}
	}

	sending := ui.Spinner("Sending to Cloudflare")
	err = w.wrangler.Apply(ctx, values, removed)
	sending.Stop()
	if err != nil {
		return 0, err
	}
	ui.Success("Sent %d secret%s to %s", len(values), plural(len(values)), ui.Bold(w.wrangler.Target()))
	if len(removed) > 0 {
		ui.Success("Deleted %d from the Worker", len(removed))
	}
	if w.wrangler.versioned {
		ui.Warn("This Worker uses gradual deployments, so the secrets went to a new version. Deploy it with wrangler versions deploy.")
	}
	return len(values), nil
}

// WorkersDelete deletes one secret from the Worker.
func WorkersDelete(ctx context.Context, w *wrangler, in io.Reader, out io.Writer, key string, yes bool) error {
	if err := w.signedIn(ctx); err != nil {
		return err
	}
	ui := NewUI(out)
	if !yes && !confirmWith(ui, in, fmt.Sprintf("Delete %s from %s?", key, w.Target())) {
		return errors.New("not deleted; pass --yes to delete without asking")
	}
	if err := w.Apply(ctx, nil, []string{key}); err != nil {
		return err
	}
	ui.Success("Deleted %s from %s", key, ui.Bold(w.Target()))
	return nil
}

// devEnvironment is what wrangler dev keeps from envi's own environment: what node and wrangler need
// to start. With CLOUDFLARE_INCLUDE_PROCESS_ENV, every variable it's given becomes a binding, so the
// rest of the shell's environment stays out.
var devEnvironment = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "COLORTERM", "TMPDIR", "TMP", "TEMP", "LANG", "TZ",
	"NO_COLOR", "FORCE_COLOR", "CI", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "APPDATA", "LOCALAPPDATA", "USERPROFILE", "PROGRAMFILES"}
var devPrefixes = []string{"LC_", "NODE_", "NPM_", "CLOUDFLARE_", "WRANGLER_", "XDG_"}

func devBase(parent []string) []string {
	var out []string
	for _, kv := range parent {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		keep := false
		for _, n := range devEnvironment {
			keep = keep || upper == n
		}
		for _, p := range devPrefixes {
			keep = keep || strings.HasPrefix(upper, p)
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

// WorkersDev runs wrangler dev with the origin's secrets in its environment and returns its exit status.
func WorkersDev(w worker, errOut io.Writer, src workerSource, f workersFlags) (int, error) {
	values, _ := selected(src, workersFlags{only: f.only, except: f.except}, workerConfig{})
	env, injected, skipped := composeEnv(devBase(os.Environ()), values, false)
	env = append(env, "CLOUDFLARE_INCLUDE_PROCESS_ENV=true")
	ui := NewUI(errOut)
	for _, k := range skipped {
		ui.Warn("Skipped %q: a secret name cannot contain '=' or a null byte.", k)
	}
	if _, err := os.Stat(w.wrangler.dir + string(os.PathSeparator) + ".dev.vars"); err == nil {
		ui.Warn(".dev.vars is here too; wrangler loads it, and its values win over Envi's.")
	}
	ui.Print(ui.Dim(fmt.Sprintf("%s · %d secret%s injected", src.label, injected, plural(injected))))
	argv := append([]string{w.wrangler.bin, "dev"}, w.wrangler.flags()...)
	return execute(append(argv, f.rest...), env)
}

// WorkersDeploy pushes, then runs wrangler deploy. wrangler creates a Worker that doesn't exist yet
// when it is given secrets, so a first deploy works the same way.
func WorkersDeploy(ctx context.Context, w worker, in io.Reader, out io.Writer, src workerSource, f workersFlags) (int, error) {
	if _, err := WorkersPush(ctx, w, in, out, src, f); err != nil || f.dryRun {
		return 0, err
	}
	argv := append([]string{w.wrangler.bin, "deploy"}, w.wrangler.flags()...)
	return execute(append(argv, f.rest...), os.Environ())
}
