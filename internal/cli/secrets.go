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
	"strings"

	projectctx "shellhaki/envi/internal/cli/project"
)

func loadContext(dir string) (projectctx.Context, error) { return projectctx.Load(dir) }
func envPath(dir string) string                          { return filepath.Join(dir, ".env") }

func parseEnv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	s := bufio.NewScanner(r)
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
		out[k] = strings.Trim(strings.TrimSpace(line[i+1:]), "\"")
	}
	return out, s.Err()
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
		if _, e = fmt.Fprintf(f, "%s=%s\n", k, values[k]); e != nil {
			return e
		}
	}
	return nil
}

func Pull(ctx context.Context, c Client, dir string) error {
	x, e := loadContext(dir)
	if e != nil {
		return e
	}
	var snapshot struct {
		Values   map[string]string `json:"values"`
		Revision int64             `json:"revision"`
	}
	if e = c.Do(ctx, "GET", "/environments/"+x.Environment.ID+"/secrets/snapshot", nil, &snapshot); e != nil {
		return e
	}
	if e = writeEnv(envPath(dir), snapshot.Values); e != nil {
		return e
	}
	x.Environment.Revision = snapshot.Revision
	return projectctx.Write(dir, x)
}
func Push(ctx context.Context, c Client, dir string) error {
	x, e := loadContext(dir)
	if e != nil {
		return e
	}
	f, e := os.Open(envPath(dir))
	if os.IsNotExist(e) {
		return errors.New(".env not found")
	}
	if e != nil {
		return e
	}
	defer f.Close()
	values, e := parseEnv(f)
	if e != nil {
		return e
	}
	var result struct {
		Revision int64 `json:"revision"`
	}
	if e = c.Do(ctx, "PUT", "/environments/"+x.Environment.ID+"/secrets/snapshot", map[string]any{"values": values, "expected_revision": x.Environment.Revision}, &result); e != nil {
		return e
	}
	x.Environment.Revision = result.Revision
	return projectctx.Write(dir, x)
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
