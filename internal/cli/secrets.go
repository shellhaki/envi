package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	projectctx "shellhaki/envi/internal/cli/project"
)

func loadContext(dir string) (projectctx.Context, error) { return projectctx.Load(dir) }
func envPath(dir string) string                          { return filepath.Join(dir, ".env") }

func parseEnv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	s := bufio.NewScanner(r)
	// Allow long lines (up to 4MB): a private key on one line is bigger than bufio's 64KB default.
	s.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			return nil, fmt.Errorf("malformed .env line")
		}
		k := strings.TrimSpace(line[:i])
		if k == "" {
			return nil, errors.New("empty .env key")
		}
		out[k] = unquoteValue(strings.TrimSpace(line[i+1:]))
	}
	return out, s.Err()
}

// unquoteValue reverses quoteValue.
func unquoteValue(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
		// Not valid escape syntax, so just strip the quotes.
		return v[1 : len(v)-1]
	}
	return v
}

// quoteValue decides whether a value can be written bare.
func quoteValue(v string) string {
	if v == "" || (!strings.ContainsAny(v, "\n\r\"\\") && strings.TrimSpace(v) == v) {
		return v
	}
	return strconv.Quote(v)
}

// renderEnv turns secrets into .env text, sorted by key.
func renderEnv(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, quoteValue(values[k]))
	}
	return b.String()
}

func writeEnv(path string, values map[string]string) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = io.WriteString(f, renderEnv(values)); e != nil {
		return e
	}
	return nil
}

// fetchSnapshot reads an environment's current secrets and the revision they were read at.
func fetchSnapshot(ctx context.Context, c Client, envID string) (map[string]string, int64, error) {
	var snapshot struct {
		Values   map[string]string `json:"values"`
		Revision int64             `json:"revision"`
	}
	if e := c.Do(ctx, "GET", "/environments/"+envID+"/secrets/snapshot", nil, &snapshot); e != nil {
		return nil, 0, e
	}
	if snapshot.Values == nil {
		snapshot.Values = map[string]string{}
	}
	return snapshot.Values, snapshot.Revision, nil
}

// Pull writes the environment's secrets to .env and returns how many.
func Pull(ctx context.Context, c Client, dir string) (int, error) {
	x, e := loadContext(dir)
	if e != nil {
		return 0, e
	}
	values, revision, e := fetchSnapshot(ctx, c, x.Environment.ID)
	if e != nil {
		return 0, e
	}
	if e = writeEnv(envPath(dir), values); e != nil {
		return 0, e
	}
	x.Environment.Revision = revision
	return len(values), projectctx.Write(dir, x)
}

// Push sends a local env file up, returning how many secrets were sent.
func Push(ctx context.Context, c Client, dir, file string, force, clean bool) (int, error) {
	x, e := loadContext(dir)
	if e != nil {
		return 0, e
	}
	path := resolveEnvFile(dir, file)
	values, e := readEnvFile(path)
	if e != nil {
		return 0, e
	}
	expected := x.Environment.Revision
	if force {
		if expected, e = currentRevision(ctx, c, x.Environment.ID); e != nil {
			return 0, e
		}
	}
	var result struct {
		Revision int64 `json:"revision"`
	}
	if e = c.Do(ctx, "PUT", "/environments/"+x.Environment.ID+"/secrets/snapshot", map[string]any{"values": values, "expected_revision": expected}, &result); e != nil {
		return 0, withForceHint(e)
	}
	x.Environment.Revision = result.Revision
	if e = projectctx.Write(dir, x); e != nil {
		return 0, e
	}
	if clean {
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return len(values), fmt.Errorf("pushed, but could not remove %s: %w", filepath.Base(path), e)
		}
	}
	return len(values), nil
}

// resolveEnvFile returns the named file (relative to the working directory) or the project's .env.
func resolveEnvFile(dir, file string) string {
	file = strings.TrimSpace(file)
	switch {
	case file == "":
		return envPath(dir)
	case filepath.IsAbs(file):
		return file
	default:
		return filepath.Join(dir, file)
	}
}

func readEnvFile(path string) (map[string]string, error) {
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil, fmt.Errorf("%s not found", filepath.Base(path))
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return parseEnv(f)
}

func currentRevision(ctx context.Context, c Client, envID string) (int64, error) {
	_, revision, e := fetchSnapshot(ctx, c, envID)
	return revision, e
}

// withForceHint turns a stale-revision rejection into advice: pull first, or push --force.
func withForceHint(e error) error {
	var api *APIError
	if errors.As(e, &api) && api.Code == "stale_revision" {
		return errors.New("remote secrets have changed since your last pull; run envi pull to take them, or envi push --force to overwrite them with your local file")
	}
	return e
}

func Diff(ctx context.Context, c Client, dir string, out io.Writer) error {
	x, e := loadContext(dir)
	if e != nil {
		return e
	}
	f, e := os.Open(envPath(dir))
	if e != nil {
		return e
	}
	defer f.Close()
	local, e := parseEnv(f)
	if e != nil {
		return e
	}
	remote, _, e := fetchSnapshot(ctx, c, x.Environment.ID)
	if e != nil {
		return e
	}
	keys := make([]string, 0, len(local)+len(remote))
	seen := map[string]bool{}
	for key := range local {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range remote {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		lv, lok := local[key]
		rv, rok := remote[key]
		switch {
		case !rok:
			fmt.Fprintln(out, "added", key)
		case !lok:
			fmt.Fprintln(out, "removed", key)
		case lv != rv:
			fmt.Fprintln(out, "changed", key)
		}
	}
	return nil
}

// fetchValues reads secrets by name rather than by environment id, for callers with no envi.toml.
func fetchValues(ctx context.Context, c Client, project, environment string) (map[string]string, string, error) {
	query := url.Values{}
	if project != "" {
		query.Set("project", project)
	}
	if environment != "" {
		query.Set("environment", environment)
	}
	path := "/values"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var reply struct {
		Project     string            `json:"project"`
		Environment string            `json:"environment"`
		Values      map[string]string `json:"values"`
	}
	if e := c.Do(ctx, "GET", path, nil, &reply); e != nil {
		var api *APIError
		if errors.As(e, &api) && api.Status == 400 {
			return nil, "", errors.New("no envi.toml here, so envi needs to be told what to read: set ENVI_PROJECT (and ENVI_ENVIRONMENT), or use a service token, or run envi init")
		}
		return nil, "", e
	}
	if reply.Values == nil {
		reply.Values = map[string]string{}
	}
	return reply.Values, reply.Project + " · " + reply.Environment, nil
}
