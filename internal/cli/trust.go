package cli

// Which folders may load their secrets automatically.
//
// The shell hook loads secrets the moment you cd into a folder, and every
// command run there inherits them. A repository someone else wrote can carry
// an envi.toml pointing at an environment you have access to; if that loaded
// on its own, their install script would be handed your keys. So nothing loads
// until you have said `envi allow` for that folder, and the permission is tied
// to the project and environment its envi.toml names: point it anywhere else
// and it must be allowed again. The revision recorded in the same file is left
// out, or every push would take the permission away.

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
	// Hash identifies the project and environment that were allowed; see
	// trustKey.
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

// epochPath holds a counter every shell's hook compares against. Allowing,
// denying or reloading bumps it, which is how a command run in one shell
// reaches the hook in every open shell without touching their environments.
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

// findRoot is the nearest folder at or above dir holding an envi.toml, so the
// secrets stay loaded in a project's subfolders. Empty when there is none.
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

// trustKey is what a permission is tied to: which project and environment
// envi.toml points at, and nothing else in the file.
func trustKey(x projectctx.Context) string {
	sum := sha256.Sum256([]byte(x.Project.ID + "\n" + x.Environment.ID))
	return hex.EncodeToString(sum[:])
}
