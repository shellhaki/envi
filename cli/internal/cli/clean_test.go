package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanFixture(t *testing.T, contents string, remote map[string]string) (string, Client) {
	t.Helper()
	d := ctxDir(t)
	if contents != "" {
		if err := os.WriteFile(filepath.Join(d, ".env"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var sent map[string]any
	srv := snapshotServerValues(t, 4, remote, &sent)
	return d, Client{BaseURL: srv}
}

// Deleting a file the server already has is the whole point.
func TestCleanRemovesAPushedFile(t *testing.T) {
	d, c := cleanFixture(t, "A=1\nB=2\n", map[string]string{"A": "1", "B": "2"})
	var out bytes.Buffer
	if err := Clean(context.Background(), c, d, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, ".env")); !os.IsNotExist(err) {
		t.Fatal(".env survived")
	}
}

// Anything the server does not have would be gone for good, so refuse and say
// exactly what would have been lost.
func TestCleanRefusesUnpushedChanges(t *testing.T) {
	d, c := cleanFixture(t, "A=1\nNEW=2\n", map[string]string{"A": "1"})
	var out bytes.Buffer
	err := Clean(context.Background(), c, d, "", false, &out)
	if err == nil {
		t.Fatal("deleted a file with unpushed changes")
	}
	for _, want := range []string{"NEW", "envi push", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err)
		}
	}
	if _, statErr := os.Stat(filepath.Join(d, ".env")); statErr != nil {
		t.Fatal("the file was deleted despite the refusal")
	}
}

// A changed value counts as unpushed, not just a new key.
func TestCleanRefusesAChangedValue(t *testing.T) {
	d, c := cleanFixture(t, "A=changed\n", map[string]string{"A": "1"})
	var out bytes.Buffer
	if err := Clean(context.Background(), c, d, "", false, &out); err == nil || !strings.Contains(err.Error(), "changed A") {
		t.Fatalf("got %v, want a refusal naming the changed key", err)
	}
}

func TestCleanForceDeletesAnyway(t *testing.T) {
	d, c := cleanFixture(t, "A=1\nNEW=2\n", map[string]string{"A": "1"})
	var out bytes.Buffer
	if err := Clean(context.Background(), c, d, "", true, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, ".env")); !os.IsNotExist(err) {
		t.Fatal("--force did not delete")
	}
}

// Nothing to do is not a failure: running clean twice must be safe.
func TestCleanWithNoFile(t *testing.T) {
	d, c := cleanFixture(t, "", map[string]string{"A": "1"})
	var out bytes.Buffer
	if err := Clean(context.Background(), c, d, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing to clean") {
		t.Fatalf("output was %q", out.String())
	}
}

func TestCleanNamedFile(t *testing.T) {
	d, c := cleanFixture(t, "A=1\n", map[string]string{"A": "1"})
	other := filepath.Join(d, ".env.test")
	if err := os.WriteFile(other, []byte("A=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Clean(context.Background(), c, d, ".env.test", false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal(".env.test survived")
	}
	// The default file must be untouched when another was named.
	if _, err := os.Stat(filepath.Join(d, ".env")); err != nil {
		t.Fatal("cleaning a named file removed .env too")
	}
}
