package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CompareVersions orders two semver-ish versions, tolerating a leading v and a pre-release suffix.
func CompareVersions(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := 0; i < 3; i++ {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	// A pre-release (1.0.0-rc1) comes before its release (1.0.0).
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	case apre < bpre:
		return -1
	default:
		return 1
	}
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(part)
	}
	return out, pre
}

// UpdateCheck reports whether a newer release exists, without touching disk.
func UpdateCheck(ctx context.Context, ui UI, current string) error {
	ui.Step("Checking for updates...")
	install := InstallMethod{Kind: "script"}
	if self, err := os.Executable(); err == nil {
		install = DetectInstall(self)
	}
	release, err := latestFor(ctx, install)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(release.Tag, "v")
	if IsDevBuild(current) {
		ui.Warn("This is a development build; latest release is %s.", ui.Bold(latest))
		return nil
	}
	switch CompareVersions(current, latest) {
	case -1:
		ui.Success("Update available: %s → %s", current, ui.Bold(latest))
		ui.Print("  Run %s to install it.", ui.Bold("envi update now"))
	case 1:
		ui.Warn("This build (%s) is ahead of the latest release (%s).", current, latest)
	default:
		ui.Success("Up to date (%s).", current)
	}
	return nil
}

// UpdateNow replaces the running binary with the latest release.
func UpdateNow(ctx context.Context, ui UI, current string, force bool) error {
	if IsDevBuild(current) && !force {
		return errors.New("this is a development build; run with --force to overwrite it with the latest release")
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("couldn't locate the running binary: %w", err)
	}
	// Replace the real binary, not the symlink pointing at it.
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	install := DetectInstall(self)

	ui.Step("Checking for updates...")
	release, err := latestFor(ctx, install)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(release.Tag, "v")
	if !force && !IsDevBuild(current) && CompareVersions(current, latest) >= 0 {
		ui.Success("Already up to date (%s).", current)
		return nil
	}

	// An npm install is owned by the package manager.
	if install.Kind == "npm" {
		return updateThroughPackageManager(ctx, ui, install, current, latest)
	}

	dir := filepath.Dir(self)
	if err = checkWritable(dir); err != nil {
		return err
	}
	ui.Step("Updating %s → %s", current, ui.Bold(latest))

	// Download next to the binary so the final rename stays on one filesystem and is atomic.
	staging, err := os.MkdirTemp(dir, ".envi-update-")
	if err != nil {
		return fmt.Errorf("couldn't create a staging directory next to %s: %w", self, err)
	}
	defer os.RemoveAll(staging)

	fresh, err := FetchBinary(ctx, ui, release.Tag, staging)
	if err != nil {
		return err
	}
	if err = verifyBinary(ctx, fresh, latest); err != nil {
		return err
	}

	backup := self + ".old"
	_ = os.Remove(backup)
	// Rename rather than delete.
	if err = os.Rename(self, backup); err != nil {
		return fmt.Errorf("couldn't move the current binary aside: %w", err)
	}
	if err = os.Rename(fresh, self); err != nil {
		// Put it back before returning, or the machine is left with no envi.
		if restore := os.Rename(backup, self); restore != nil {
			return fmt.Errorf("update failed (%v) and the original could not be restored from %s: %v", err, backup, restore)
		}
		return fmt.Errorf("couldn't install the new binary: %w", err)
	}
	if err = os.Chmod(self, 0o755); err != nil {
		ui.Warn("Installed, but couldn't set the executable bit: %v", err)
	}
	// Windows can't delete a running binary; the next update overwrites it.
	if err = os.Remove(backup); err != nil && runtime.GOOS != "windows" {
		ui.Warn("Left the previous binary at %s", backup)
	}

	ui.Success("Updated to %s", ui.Bold(latest))
	return nil
}

// checkWritable fails before the download rather than after it.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".envi-write-test-")
	if err != nil {
		return fmt.Errorf("no write access to %s; reinstall with the install script, or re-run with elevated permissions", dir)
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

// verifyBinary runs the freshly downloaded executable and confirms it reports the version we expect.
func verifyBinary(ctx context.Context, path, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("the downloaded binary would not run, so the update was abandoned: %w", err)
	}
	got := strings.TrimSpace(string(out))
	if strings.TrimPrefix(got, "v") != strings.TrimPrefix(want, "v") {
		return fmt.Errorf("the downloaded binary reports version %q, expected %q; update abandoned", got, want)
	}
	return nil
}

// updateThroughPackageManager updates an npm install with its own package manager.
func updateThroughPackageManager(ctx context.Context, ui UI, install InstallMethod, current, latest string) error {
	command := install.UpdateCommand(latest)
	printable := strings.Join(command, " ")

	if !install.Available() {
		ui.Warn("Envi was installed with %s, which isn't on your PATH.", install.Manager)
		ui.Print("Update it with:\n  %s", ui.Bold(printable))
		return nil
	}

	ui.Step("Updating %s → %s with %s", current, ui.Bold(latest), install.Manager)
	run := exec.CommandContext(ctx, command[0], command[1:]...)
	run.Stdout, run.Stderr = ui.Out, ui.Out
	if err := run.Run(); err != nil {
		ui.Warn("%s did not finish: %v", install.Manager, err)
		ui.Print("Try it yourself with:\n  %s", ui.Bold(printable))
		return err
	}
	ui.Success("Updated to %s", ui.Bold(latest))
	return nil
}
