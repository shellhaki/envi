package cli

import (
	"os/exec"
	"runtime"
)

// openURL tries to open a URL in the user's default browser. It is best-effort:
// the URL is always printed too, so a failure here is not fatal to auth. The
// child is started detached and never waited on.
func openURL(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
		args = []string{url}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		name = "xdg-open"
		args = []string{url}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release so we don't leak the process handle; we never read its exit.
	return cmd.Process.Release()
}
