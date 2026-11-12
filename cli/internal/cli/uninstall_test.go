package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Declining must leave everything exactly as it was.
func TestUninstallCancelledLeavesEverythingInPlace(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "envi")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(config, "session.db")
	if err := os.WriteFile(marker, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVI_CONFIG_DIR", config)

	var out bytes.Buffer
	if err := Uninstall(UI{Out: &out}, strings.NewReader("n\n"), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("declining the prompt still removed the session store")
	}
	if !strings.Contains(out.String(), "Cancelled") {
		t.Fatalf("expected a cancellation notice, got %q", out.String())
	}
}

// An empty answer is not consent.
func TestUninstallTreatsBlankAnswerAsNo(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "envi")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVI_CONFIG_DIR", config)
	var out bytes.Buffer
	if err := Uninstall(UI{Out: &out}, strings.NewReader("\n"), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatal("a blank answer removed the config directory")
	}
}

// ENVI_CONFIG_DIR is user-supplied, so a recursive delete must not follow it
// somewhere arbitrary.
func TestUninstallRefusesConfigDirThatIsNotEnvis(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, "important-documents")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(stray, "taxes.pdf")
	if err := os.WriteFile(keep, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVI_CONFIG_DIR", stray)

	var out bytes.Buffer
	// Answering yes; the guard, not the prompt, is what has to hold here. The
	// binary removal will fail in the test process, which is fine — the point
	// is that the stray directory survives.
	_ = Uninstall(UI{Out: &out}, strings.NewReader("y\n"), false, false)
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("uninstall deleted a directory that was not an envi config dir")
	}
	if !strings.Contains(out.String(), "not an envi config directory") {
		t.Fatalf("expected the guard to explain itself, got %q", out.String())
	}
}

// --keep-config exists so someone reinstalling does not have to sign in again.
func TestUninstallKeepConfigLeavesTheSession(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "envi")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(config, "session.db")
	if err := os.WriteFile(marker, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVI_CONFIG_DIR", config)

	var out bytes.Buffer
	_ = Uninstall(UI{Out: &out}, strings.NewReader("y\n"), true, true)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("--keep-config still removed the session store")
	}
}

// The listing has to appear before anything is removed, and it has to say that
// server-side data is untouched.
func TestUninstallExplainsScopeBeforeActing(t *testing.T) {
	config := filepath.Join(t.TempDir(), "envi")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVI_CONFIG_DIR", config)
	var out bytes.Buffer
	_ = Uninstall(UI{Out: &out}, strings.NewReader("n\n"), false, false)
	for _, want := range []string{"This will remove:", "not affected", "envi logout"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the prompt to mention %q, got:\n%s", want, out.String())
		}
	}
}
