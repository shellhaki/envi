package cli

// How this copy of envi was installed, and therefore how to update it.
//
// There is no marker file to rely on: most installs predate this code, so the
// answer has to be readable from where the binary sits. npm puts it under
// node_modules, Homebrew under its own prefix, and the install script puts it
// wherever the user asked.

import (
	"os/exec"
	"path/filepath"
	"strings"
)

type InstallMethod struct {
	// Kind is "npm", "homebrew", or "script".
	Kind string
	// Manager is which package manager owns an npm install: npm, pnpm, yarn
	// or bun. Empty for the other kinds.
	Manager string
}

// Package is the npm package name an npm install came from.
const NPMPackage = "@shellhaki/envi-cli"

// DetectInstall works out how the binary at path was installed.
func DetectInstall(path string) InstallMethod {
	// Symlinks are the norm for globally installed commands, and the link
	// itself says nothing; the file it points at does.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	// Normalise separators by hand rather than with filepath.ToSlash, which
	// does nothing off Windows: a Windows path can reach this code from a
	// config file or a test on any platform.
	lower := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))

	if strings.Contains(lower, "/node_modules/") {
		return InstallMethod{Kind: "npm", Manager: managerFromPath(lower)}
	}
	if strings.Contains(lower, "/cellar/") || strings.Contains(lower, "/homebrew/") {
		return InstallMethod{Kind: "homebrew"}
	}
	return InstallMethod{Kind: "script"}
}

// managerFromPath reads the package manager off the global install directory,
// each of which keeps its packages somewhere recognisable.
func managerFromPath(lower string) string {
	switch {
	case strings.Contains(lower, "/.bun/"):
		return "bun"
	case strings.Contains(lower, "/pnpm/"), strings.Contains(lower, "/.pnpm/"):
		return "pnpm"
	case strings.Contains(lower, "/yarn/"), strings.Contains(lower, "/.yarn/"):
		return "yarn"
	default:
		return "npm"
	}
}

// UpdateCommand is the command that updates an npm install in place, as
// program plus arguments. Only meaningful when Kind is "npm".
func (m InstallMethod) UpdateCommand(version string) []string {
	target := NPMPackage
	if version != "" {
		target += "@" + strings.TrimPrefix(version, "v")
	}
	switch m.Manager {
	case "bun":
		return []string{"bun", "add", "-g", target}
	case "pnpm":
		return []string{"pnpm", "add", "-g", target}
	case "yarn":
		return []string{"yarn", "global", "add", target}
	default:
		return []string{"npm", "install", "-g", target}
	}
}

// Available reports whether the package manager is actually on PATH, so the
// caller can fall back to printing the command rather than failing oddly.
func (m InstallMethod) Available() bool {
	cmd := m.UpdateCommand("")
	if len(cmd) == 0 {
		return false
	}
	_, err := exec.LookPath(cmd[0])
	return err == nil
}
