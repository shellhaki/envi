package cli

// The shell hook (envi hook, allow, deny, reload) was removed in v1.5.1. What's left only cleans up after it:
// an old shell startup file still runs `envi hook <shell>`, and a shell opened before the update still runs `envi export`.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	hookBegin = "# >>> envi hook >>>"
	hookEnd   = "# <<< envi hook <<<"
)

// runOldHook answers the old hook's calls with no output, so the shell runs nothing, and removes the hook on first call.
func (a App) runOldHook(args []string) int {
	if args[0] == "hook" {
		ui := NewUI(a.Err)
		for _, f := range removeOldHook() {
			ui.Print("%s", ui.Dim("envi: the shell hook was removed; took it out of "+f+". Use `envi run -- <command>` instead."))
		}
	}
	return ExitOK
}

// removeOldHook takes the hook out of every shell startup file and deletes the files it kept. It returns the startup files it changed.
func removeOldHook() []string {
	var changed []string
	for _, f := range oldHookStartupFiles() {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), hookBegin) {
			continue
		}
		if filepath.Base(f) == "envi.fish" {
			err = os.Remove(f)
		} else {
			err = os.WriteFile(f, []byte(withoutHook(string(b))), 0o644)
		}
		if err == nil {
			changed = append(changed, f)
		}
	}
	if dir, err := ConfigDir(); err == nil {
		for _, name := range []string{"allowed.json", "hook-epoch", "hook-tip-shown"} {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return changed
}

// removeOldHookFor is removeOldHook with a line per file, for envi uninstall.
func removeOldHookFor(out io.Writer) {
	ui := NewUI(out)
	for _, f := range removeOldHook() {
		ui.Success("Removed the old envi hook from %s", f)
	}
}

// withoutHook cuts the marked block, and the blank line added before it.
func withoutHook(s string) string {
	start := strings.Index(s, hookBegin)
	end := strings.Index(s, hookEnd)
	if start < 0 || end < start {
		return s
	}
	end += len(hookEnd)
	if end < len(s) && s[end] == '\n' {
		end++
	}
	before := strings.TrimRight(s[:start], "\n")
	after := s[end:]
	if before == "" {
		return after
	}
	return before + "\n" + after
}

// oldHookStartupFiles are every file the hook could have been installed in.
func oldHookStartupFiles() []string {
	home, _ := os.UserHomeDir()
	zdot := os.Getenv("ZDOTDIR")
	if zdot == "" {
		zdot = home
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	files := []string{
		filepath.Join(zdot, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(config, "fish", "conf.d", "envi.fish"),
	}
	return append(files, pwshProfiles()...)
}

var (
	pwshOnce  sync.Once
	pwshFound []string
)

// pwshProfiles are this user's PowerShell profile files (PowerShell 7 and Windows PowerShell).
var pwshProfiles = func() []string {
	pwshOnce.Do(func() {
		seen := map[string]bool{}
		for _, exe := range []string{"pwsh", "powershell"} {
			path, err := exec.LookPath(exe)
			if err != nil {
				continue
			}
			out, err := exec.Command(path, "-NoProfile", "-NonInteractive", "-Command",
				"$PROFILE.CurrentUserCurrentHost").Output()
			if p := strings.TrimSpace(string(out)); err == nil && p != "" && !seen[p] {
				seen[p] = true
				pwshFound = append(pwshFound, p)
			}
		}
		if len(pwshFound) == 0 && runtime.GOOS == "windows" {
			home, _ := os.UserHomeDir()
			pwshFound = []string{filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")}
		}
	})
	return pwshFound
}
