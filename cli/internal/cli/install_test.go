package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the binary sits is the only evidence available for installs that
// predate any marker, which is all of them today.
func TestDetectInstall(t *testing.T) {
	cases := []struct {
		path    string
		kind    string
		manager string
	}{
		{"/usr/local/lib/node_modules/@shellhaki/envi-cli-darwin-arm64/envi", "npm", "npm"},
		{"/Users/x/.bun/install/global/node_modules/@shellhaki/envi-cli-linux-x64/envi", "npm", "bun"},
		{"/Users/x/Library/pnpm/global/5/node_modules/@shellhaki/envi-cli-darwin-x64/envi", "npm", "pnpm"},
		{"/Users/x/.yarn/global/node_modules/@shellhaki/envi-cli-darwin-x64/envi", "npm", "yarn"},
		{"/opt/homebrew/Cellar/envi/0.3.0/bin/envi", "homebrew", ""},
		{"/Users/x/.local/bin/envi", "script", ""},
		{"/usr/local/bin/envi", "script", ""},
		{`C:\Users\x\AppData\Roaming\npm\node_modules\@shellhaki\envi-cli-win32-x64\envi.exe`, "npm", "npm"},
	}
	for _, c := range cases {
		got := DetectInstall(c.path)
		if got.Kind != c.kind || got.Manager != c.manager {
			t.Errorf("DetectInstall(%q) = %+v, want kind=%q manager=%q", c.path, got, c.kind, c.manager)
		}
	}
}

// Each manager has its own way of saying "install this globally"; getting it
// wrong would print a command that silently does nothing useful.
func TestUpdateCommand(t *testing.T) {
	cases := map[string]string{
		"npm":  "npm install -g @shellhaki/envi-cli@0.4.0",
		"bun":  "bun add -g @shellhaki/envi-cli@0.4.0",
		"pnpm": "pnpm add -g @shellhaki/envi-cli@0.4.0",
		"yarn": "yarn global add @shellhaki/envi-cli@0.4.0",
	}
	for manager, want := range cases {
		got := strings.Join(InstallMethod{Kind: "npm", Manager: manager}.UpdateCommand("v0.4.0"), " ")
		if got != want {
			t.Errorf("%s: got %q, want %q", manager, got, want)
		}
	}
	// No version means "latest", with no stray @ on the end.
	if got := strings.Join(InstallMethod{Kind: "npm", Manager: "npm"}.UpdateCommand(""), " "); !strings.HasSuffix(got, NPMPackage) {
		t.Errorf("unversioned command = %q", got)
	}
}

// A symlinked command says nothing about its install; the file it points at does.
func TestDetectInstallFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "node_modules", "@shellhaki", "envi-cli-darwin-arm64")
	mustMkdirAll(t, real)
	binary := filepath.Join(real, "envi")
	mustWrite(t, binary)
	link := filepath.Join(dir, "envi")
	mustSymlink(t, binary, link)

	if got := DetectInstall(link); got.Kind != "npm" {
		t.Fatalf("through a symlink: got %+v, want npm", got)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
