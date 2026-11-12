package cli

// Which folders may load their secrets automatically. Nothing loads until `envi allow`, so a cloned repo
// can't point its envi.toml at your environments and hand your secrets to its install scripts.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	projectctx "shellhaki/envi/internal/cli/project"
)

// Allowed is one folder the hook may load.
type Allowed struct {
	// Hash identifies the project and environment that were allowed; see trustKey.
	Hash        string    `json:"hash"`
	Project     string    `json:"project"`
	Environment string    `json:"environment"`
	Production  bool      `json:"production"`
	AllowedAt   time.Time `json:"allowed_at"`
}

type trustStore struct{ path string }

func defaultTrustStore() (trustStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return trustStore{}, err
	}
	return trustStore{path: filepath.Join(dir, "allowed.json")}, nil
}

func (s trustStore) load() (map[string]Allowed, error) {
	out := map[string]Allowed{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, errors.New("allowed.json is damaged; delete it and run envi allow again")
	}
	return out, nil
}

func (s trustStore) save(all map[string]Allowed) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// epochPath holds a counter every shell's hook compares against.
func epochPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hook-epoch"), nil
}

func readEpoch() string {
	p, err := epochPath()
	if err != nil {
		return "0"
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "0"
	}
	return strings.TrimSpace(string(b))
}

func bumpEpoch() error {
	p, err := epochPath()
	if err != nil {
		return err
	}
	n, _ := strconv.ParseInt(readEpoch(), 10, 64)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strconv.FormatInt(n+1, 10)), 0o600)
}

// findRoot is the nearest folder at or above dir with an envi.toml, or "".
func findRoot(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "envi.toml")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// trustKey identifies the project and environment envi.toml points at.
func trustKey(x projectctx.Context) string {
	sum := sha256.Sum256([]byte(x.Project.ID + "\n" + x.Environment.ID))
	return hex.EncodeToString(sum[:])
}
