package session

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound reports that no session has been stored yet, as distinct from a
// store that exists but could not be read.
var ErrNotFound = errors.New("no stored session")

// Session is a locally cached login. AccessExpiry lets the CLI refresh before a
// request fails rather than after.
type Session struct {
	Access, Refresh string
	AccessExpiry    time.Time
}

type Store struct{ Path string }

func (s Store) Save(v Session) error {
	// The directory is the real boundary: SQLite writes -journal/-wal sidecars
	// next to the database under its own umask, and those can hold token bytes.
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	// Create the file restricted before SQLite opens it, so tokens are never
	// briefly world-readable on a fresh login.
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(s.Path, 0600); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", s.Path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS session (id INTEGER PRIMARY KEY CHECK(id=1), access TEXT NOT NULL, refresh TEXT NOT NULL, access_expiry INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	// Upgrade databases written before access_expiry existed; the column already
	// being present is the expected case, not a failure.
	if _, err = db.Exec(`ALTER TABLE session ADD COLUMN access_expiry INTEGER NOT NULL DEFAULT 0`); err != nil && !isDuplicateColumn(err) {
		return err
	}
	var expiry int64
	if !v.AccessExpiry.IsZero() {
		expiry = v.AccessExpiry.Unix()
	}
	_, err = db.Exec(`INSERT INTO session(id,access,refresh,access_expiry) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET access=excluded.access,refresh=excluded.refresh,access_expiry=excluded.access_expiry`, v.Access, v.Refresh, expiry)
	return err
}

func (s Store) Load() (Session, error) {
	if _, err := os.Stat(s.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	db, err := sql.Open("sqlite", s.Path)
	if err != nil {
		return Session{}, err
	}
	defer db.Close()
	var name string
	if err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='session'`).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("read session store: %w", err)
	}
	var v Session
	var expiry int64
	err = db.QueryRow(`SELECT access,refresh,COALESCE(access_expiry,0) FROM session WHERE id=1`).Scan(&v.Access, &v.Refresh, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	// A corrupt or unreadable store is a real failure and must not be reported as
	// "not logged in", which would send the user in circles through envi auth.
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	if expiry > 0 {
		v.AccessExpiry = time.Unix(expiry, 0)
	}
	return v, nil
}

// Clear removes the stored session. A missing store is already cleared.
func (s Store) Clear() error {
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}
