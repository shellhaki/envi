package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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

// Pull writes the environment's secrets to .env, returning how many were
// written so the caller can say so rather than just "complete".
func Pull(ctx context.Context, c Client, dir string) (int, error) {
	x, e := loadContext(dir)
	if e != nil {
		return 0, e
	}
	var snapshot struct {
		Values   map[string]string `json:"values"`
		Revision int64             `json:"revision"`
	}
	if e = c.Do(ctx, "GET", "/environments/"+x.Environment.ID+"/secrets/snapshot", nil, &snapshot); e != nil {
		return 0, e
	}
	if e = writeEnv(envPath(dir), snapshot.Values); e != nil {
		return 0, e
	}
	x.Environment.Revision = snapshot.Revision
	return len(snapshot.Values), projectctx.Write(dir, x)
}

// Push sends local .env changes up, returning how many secrets were sent.
func Push(ctx context.Context, c Client, dir string) (int, error) {
	x, e := loadContext(dir)
	if e != nil {
		return 0, e
	}
	f, e := os.Open(envPath(dir))
	if os.IsNotExist(e) {
		return 0, errors.New(".env not found")
	}
	if e != nil {
		return 0, e
	}
	defer f.Close()
	values, e := parseEnv(f)
	if e != nil {
		return 0, e
	}
	var result struct {
		Revision int64 `json:"revision"`
	}
	if e = c.Do(ctx, "PUT", "/environments/"+x.Environment.ID+"/secrets/snapshot", map[string]any{"values": values, "expected_revision": x.Environment.Revision}, &result); e != nil {
		return 0, e
	}
	x.Environment.Revision = result.Revision
	return len(values), projectctx.Write(dir, x)
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
	var remote struct {
		Values map[string]string `json:"values"`
	}
	if e = c.Do(ctx, "GET", "/environments/"+x.Environment.ID+"/secrets/snapshot", nil, &remote); e != nil {
		return e
	}
	keys := make([]string, 0, len(local)+len(remote.Values))
	seen := map[string]bool{}
	for key := range local {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range remote.Values {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		lv, lok := local[key]
		rv, rok := remote.Values[key]
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
