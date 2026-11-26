package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// A platform is somewhere envi can send an origin's secrets. Cloudflare Workers is the first;
// others (Vercel, Netlify, Fly) can follow by implementing the same three calls.
type platform interface {
	// List returns the names of the secrets already set. Platforms never return values.
	List(ctx context.Context) ([]string, error)
	// Apply sets values and deletes the names in remove.
	Apply(ctx context.Context, values map[string]string, remove []string) error
	// Target describes where the secrets go, for messages.
	Target() string
}

// wrangler drives the user's own wrangler CLI, so it uses whatever account they signed in with.
type wrangler struct {
	bin    string // path to the wrangler binary
	dir    string // the project directory wrangler runs in
	env    string // the wrangler environment ("" is the top-level Worker)
	name   string // --name, when given
	config string // -c, when given
	worker string // the Worker's name, for messages
	// Versioned is set once wrangler reports gradual deployments: secrets then go to a new version.
	versioned bool
}

// bulkSize is the most secrets wrangler takes in one `secret bulk`.
const bulkSize = 100

// findWrangler looks for the project's own wrangler first (node_modules/.bin, up to the filesystem
// root, for monorepos), then for one on PATH.
func findWrangler(dir string) (string, error) {
	names := []string{"wrangler"}
	if os.PathSeparator == '\\' {
		names = []string{"wrangler.cmd", "wrangler.exe", "wrangler"}
	}
	for d := dir; ; d = filepath.Dir(d) {
		for _, n := range names {
			p := filepath.Join(d, "node_modules", ".bin", n)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p, nil
			}
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	if p, err := exec.LookPath("wrangler"); err == nil {
		return p, nil
	}
	return "", errors.New("wrangler not found; install it in this project with `npm i -D wrangler`")
}

// flags are the arguments that pick the Worker, passed on every call.
func (w *wrangler) flags() []string {
	var out []string
	if w.env != "" {
		out = append(out, "--env", w.env)
	}
	if w.name != "" {
		out = append(out, "--name", w.name)
	}
	if w.config != "" {
		out = append(out, "--config", w.config)
	}
	return out
}

// call runs wrangler with stdin and returns what it printed. Values only ever travel on stdin.
func (w *wrangler) call(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, w.bin, args...)
	cmd.Dir = w.dir
	cmd.Env = append(os.Environ(), "WRANGLER_SEND_METRICS=false")
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(out.String())
		if text == "" {
			text = err.Error()
		}
		return out.Bytes(), fmt.Errorf("wrangler %s failed:\n%s", strings.Join(args[:min(2, len(args))], " "), text)
	}
	return out.Bytes(), nil
}

// signedIn checks that wrangler can reach an account, through `wrangler login` or CLOUDFLARE_API_TOKEN.
func (w *wrangler) signedIn(ctx context.Context) error {
	out, err := w.call(ctx, nil, "whoami")
	if err != nil || strings.Contains(strings.ToLower(string(out)), "not authenticated") {
		return errors.New("wrangler is not signed in; run `wrangler login`, or set CLOUDFLARE_API_TOKEN in CI")
	}
	return nil
}

func (w *wrangler) Target() string {
	if w.env == "" {
		return w.worker
	}
	return w.worker + " (" + w.env + ")"
}

func (w *wrangler) List(ctx context.Context) ([]string, error) {
	out, err := w.call(ctx, nil, append([]string{"secret", "list", "--format", "json"}, w.flags()...)...)
	if err != nil {
		return nil, err
	}
	// wrangler may print a banner before the JSON.
	start := bytes.IndexByte(out, '[')
	if start < 0 {
		return nil, fmt.Errorf("wrangler secret list printed no JSON:\n%s", strings.TrimSpace(string(out)))
	}
	var secrets []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(bytes.NewReader(out[start:])).Decode(&secrets); err != nil {
		return nil, fmt.Errorf("could not read wrangler secret list: %w", err)
	}
	names := make([]string, 0, len(secrets))
	for _, s := range secrets {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names, nil
}

// Apply sends values in batches of bulkSize, as JSON on stdin, then deletes the names in remove one
// by one: wrangler's bulk upload only sets values.
func (w *wrangler) Apply(ctx context.Context, values map[string]string, remove []string) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += bulkSize {
		batch := map[string]string{}
		for _, k := range keys[start:min(start+bulkSize, len(keys))] {
			batch[k] = values[k]
		}
		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if err := w.secret(ctx, body, "bulk"); err != nil {
			return err
		}
	}
	for _, k := range remove {
		// wrangler asks before deleting, and answers yes itself when it isn't run from a terminal.
		if err := w.secret(ctx, nil, "delete", k); err != nil {
			return err
		}
	}
	return nil
}

// secret runs `wrangler secret <args>`, or `wrangler versions secret <args>` when the Worker uses
// gradual deployments, where changing a secret on the deployed version is refused.
func (w *wrangler) secret(ctx context.Context, stdin []byte, args ...string) error {
	if !w.versioned {
		out, err := w.call(ctx, stdin, append(append([]string{"secret"}, args...), w.flags()...)...)
		if err == nil {
			return nil
		}
		if !gradual(string(out)) {
			return err
		}
		w.versioned = true
	}
	_, err := w.call(ctx, stdin, append(append([]string{"versions", "secret"}, args...), w.flags()...)...)
	return err
}

// gradual reports whether wrangler refused a secret change because of a gradual deployment.
func gradual(output string) bool {
	o := strings.ToLower(output)
	return strings.Contains(o, "gradual") || strings.Contains(o, "isn't currently deployed") || strings.Contains(o, "versions secret")
}

// A workerConfig is the part of wrangler.toml or wrangler.jsonc envi reads: the Worker's name and
// the names in [vars], which can't also be secrets.
type workerConfig struct {
	Path string
	Name string
	Vars map[string]bool
}

// readWorkerConfig reads the config wrangler would use (the -c path, or wrangler.json, wrangler.jsonc,
// wrangler.toml in dir) for the given wrangler environment. Vars don't inherit: an environment only
// has its own.
func readWorkerConfig(dir, config, env string) (workerConfig, error) {
	path := ""
	if config != "" {
		path = config
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
	} else {
		for _, n := range []string{"wrangler.json", "wrangler.jsonc", "wrangler.toml"} {
			if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
				path = filepath.Join(dir, n)
				break
			}
		}
	}
	if path == "" {
		return workerConfig{}, errors.New("no wrangler.toml or wrangler.jsonc here; run envi workers in your Worker's directory, or pass -c")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return workerConfig{}, err
	}

	type section struct {
		Name string         `json:"name" toml:"name"`
		Vars map[string]any `json:"vars" toml:"vars"`
	}
	var doc struct {
		section
		Env map[string]section `json:"env" toml:"env"`
	}
	if strings.HasSuffix(path, ".toml") {
		err = toml.Unmarshal(b, &doc)
	} else {
		err = json.Unmarshal(stripJSONC(b), &doc)
	}
	if err != nil {
		return workerConfig{Path: path}, fmt.Errorf("could not read %s: %w", filepath.Base(path), err)
	}

	out := workerConfig{Path: path, Name: doc.Name, Vars: map[string]bool{}}
	vars := doc.Vars
	if env != "" {
		e := doc.Env[env]
		vars = e.Vars
		out.Name = doc.Name + "-" + env
		if e.Name != "" {
			out.Name = e.Name
		}
	}
	for k := range vars {
		out.Vars[k] = true
	}
	return out, nil
}

// stripJSONC removes // and /* */ comments and trailing commas, leaving strings alone. The second
// pass catches a trailing comma that had a comment after it.
func stripJSONC(in []byte) []byte { return stripPass(stripPass(in)) }

func stripPass(in []byte) []byte {
	var out []byte
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(in) && in[j] != '"' {
				if in[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(in))
			out = append(out, in[i:end]...)
			i = end - 1
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			i--
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			j := i + 1
			for j < len(in) && strings.ContainsRune(" \t\r\n", rune(in[j])) {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
