package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Uninstall removes this binary and, unless the caller keeps it, the local
// session store. It never touches anything server-side: secrets, projects and
// collaborators are untouched, and the session is revoked only if the user
// runs `envi logout` first — which is exactly what we tell them.
//
// Everything to be removed is listed before anything is removed, and the
// removal only happens on an explicit "y", so a mistyped command costs nothing.
func Uninstall(ui UI, in io.Reader, keepConfig, assumeYes bool) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("couldn't locate the running binary: %w", err)
	}
	if resolved, e := filepath.EvalSymlinks(self); e == nil {
		self = resolved
	}
	configDir, cfgErr := ConfigDir()

	ui.Print("%s", ui.Bold("This will remove:"))
	ui.Print("  %s", self)
	if !keepConfig && cfgErr == nil {
		if _, statErr := os.Stat(configDir); statErr == nil {
			ui.Print("  %s %s", configDir, ui.Dim("(saved session)"))
		}
	}
	ui.Print("")
	ui.Print("%s", ui.Dim("Your projects, environments and secrets are stored server-side and are not affected."))
	ui.Print("%s", ui.Dim("To revoke this device's session as well, run `envi logout` before uninstalling."))
	ui.Print("")

	if !assumeYes && !confirm(ui, in) {
		ui.Print("Cancelled.")
		return nil
	}

	if !keepConfig && cfgErr == nil && configDir != "" && filepath.Base(configDir) == "envi" {
		// The base-name guard is deliberate: ENVI_CONFIG_DIR is user-supplied,
		// and a recursive delete of an arbitrary path is not something this
		// command should ever be talked into.
		if err = os.RemoveAll(configDir); err != nil {
			ui.Warn("Couldn't remove %s: %v", configDir, err)
		} else {
			ui.Success("Removed the saved session")
		}
	} else if !keepConfig && cfgErr == nil {
		ui.Warn("Skipped %s: not an envi config directory", configDir)
	}

	if err = os.Remove(self); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("no permission to remove %s; re-run with elevated permissions, or delete it by hand", self)
		}
		if runtime.GOOS == "windows" {
			// A running image cannot delete itself here; renaming is the best
			// available outcome and leaves nothing on PATH.
			if renameErr := os.Rename(self, self+".old"); renameErr == nil {
				ui.Success("Uninstalled. Delete %s.old when convenient.", self)
				return nil
			}
		}
		return fmt.Errorf("couldn't remove %s: %w", self, err)
	}
	ui.Success("Uninstalled envi")
	ui.Print("%s", ui.Dim("Reinstall any time with: curl -fsSL https://install.envisecrets.com | sh"))
	return nil
}

func confirm(ui UI, in io.Reader) bool {
	fmt.Fprint(ui.Out, "Continue? [y/N] ")
	if in == nil {
		return false
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
