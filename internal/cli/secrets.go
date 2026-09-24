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
	// A private key or certificate on one quoted line comfortably exceeds
	// bufio's 64KB default, which would otherwise surface as a truncated value
	// rather than an error.
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

// unquoteValue reverses quoteValue. A double-quoted value carries escapes, so
// newlines and surrounding whitespace survive; anything else is taken
// literally, which keeps hand-written .env files working as before.
func unquoteValue(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
		// Not valid escape syntax (a hand-written "value" with a stray
		// backslash, say) — fall back to stripping the delimiters.
		return v[1 : len(v)-1]
	}
	return v
}

// quoteValue decides whether a value can be written bare. Newlines are the
// case that matters: a private key written raw spreads across lines and the
// file no longer parses at all, silently corrupting the secret it was meant to
// carry. Leading and trailing spaces are quoted for the same reason — parsing
// trims them, so writing them bare loses them.
func quoteValue(v string) string {
	if v == "" || (!strings.ContainsAny(v, "\n\r\"\\") && strings.TrimSpace(v) == v) {
		return v
	}
	return strconv.Quote(v)
}
func writeEnv(path string, values map[string]string) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, e = fmt.Fprintf(f, "%s=%s\n", k, quoteValue(values[k])); e != nil {
			return e
		}
	}
	return nil
}

// fetchSnapshot reads an environment's current secrets and the revision they
// were read at. Every command that touches the remote goes through here, so the
// request shape is stated once.
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

// Pull writes the environment's secrets to .env, returning how many were
// written so the caller can say so rather than just "complete".
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
//
// The write is a compare-and-swap against the revision recorded in envi.toml,
// so an origin that moved on since the last pull is rejected rather than
// quietly overwritten. force skips that check and writes against whatever the
// server currently holds, for when the local file is already the wanted state
// and pulling first would only drag down secrets destined to be discarded.
// clean removes the file after a successful push, so a pull-edit-push cycle
// leaves nothing plaintext behind. Nothing is deleted unless the push worked.
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

// resolveEnvFile picks the file a command reads: the project's .env unless one
// was named on the command line, in which case it is taken relative to the
// working directory.
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

// withForceHint replaces the server's compare-and-swap rejection with the two
// choices the user actually has. The bare code sends people to envi pull even
// when the remote holds nothing they want.
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

// fetchValues reads secrets by name rather than by environment id, for callers
// with no envi.toml. A service token names its own environment, so both
// arguments may be empty; anything else must say which project it means.
//
// It returns the values and a "project · environment" label for display.
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
