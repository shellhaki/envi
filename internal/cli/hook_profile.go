package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

func isDarwin() bool { return runtime.GOOS == "darwin" }

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
