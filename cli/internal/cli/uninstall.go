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

// Uninstall removes this binary and, unless the caller keeps it, the local session store.
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
	if HookInstalled() {
		ui.Print("  %s", ui.Dim("the envi hook in your shell startup file"))
	}
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

	// Remove the shell hook too.
	if HookInstalled() {
		if err := UninstallHook(ui.Out); err != nil {
			ui.Warn("Couldn't remove the shell hook: %v", err)
		}
	}

	if !keepConfig && cfgErr == nil && configDir != "" && filepath.Base(configDir) == "envi" {
		// Only delete a folder named envi: ENVI_CONFIG_DIR is user-supplied.
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
			// Windows can't delete a running binary, so rename it out of the way.
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
