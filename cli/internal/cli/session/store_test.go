package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envi", "session.db")
	s := Store{Path: path}

	if _, err := s.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing store: %v", err)
	}

	expiry := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	if err := s.Save(Session{Access: "access", Refresh: "refresh", AccessExpiry: expiry}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Load()
	if err != nil || v.Access != "access" || v.Refresh != "refresh" {
		t.Fatal(v, err)
	}
	if !v.AccessExpiry.Equal(expiry) {
		t.Fatalf("expiry = %v want %v", v.AccessExpiry, expiry)
	}

	// Tokens in cleartext on disk must not be readable by other local users, and
	// the containing directory is the boundary for SQLite's sidecar files.
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %v err = %v", info.Mode().Perm(), err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || parent.Mode().Perm() != 0700 {
		t.Fatalf("dir mode = %v err = %v", parent.Mode().Perm(), err)
	}

	// Saving again must replace the single row rather than accumulate sessions.
	if err = s.Save(Session{Access: "second", Refresh: "rotated"}); err != nil {
		t.Fatal(err)
	}
	v, err = s.Load()
	if err != nil || v.Access != "second" || v.Refresh != "rotated" {
		t.Fatal(v, err)
	}
	if !v.AccessExpiry.IsZero() {
		t.Fatalf("expiry = %v want zero", v.AccessExpiry)
	}

	if err = s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleared store: %v", err)
	}
	// Clearing an already-cleared store is not an error.
	if err = s.Clear(); err != nil {
		t.Fatal(err)
	}
}

// A store that exists but cannot be parsed must report a real error rather than
// masquerading as "not logged in", which would loop the user through envi auth.
func TestLoadReportsCorruptStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	if err := os.WriteFile(path, []byte("this is not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Store{Path: path}.Load()
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
