package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives every test in this package a throwaway home directory, so
// nothing here can read or edit the real ~/.zshrc, ~/.bashrc, fish config or
// PowerShell profile, which the shell hook and uninstall both touch.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "envi-cli-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Unsetenv("ZDOTDIR")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	profile := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	pwshProfiles = func() []string { return []string{profile} }
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
