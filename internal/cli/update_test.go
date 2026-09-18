package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.0.1", "0.0.2", -1},
		{"0.0.2", "0.0.1", 1},
		{"0.0.1", "0.0.1", 0},
		{"v0.1.0", "0.1.0", 0},
		{"0.9.0", "0.10.0", -1}, // numeric, not lexical
		{"1.0.0", "0.99.99", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
		{"0.1", "0.1.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	for _, v := range []string{"dev", "", "dev-abc123", "  dev  "} {
		if !IsDevBuild(v) {
			t.Errorf("IsDevBuild(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"0.0.1", "v1.2.3", "1.0.0-rc1"} {
		if IsDevBuild(v) {
			t.Errorf("IsDevBuild(%q) = true, want false", v)
		}
	}
}

// A development build must not be silently replaced by a release: that would
// discard whatever the developer just built.
func TestUpdateNowRefusesDevBuildWithoutForce(t *testing.T) {
	var out bytes.Buffer
	err := UpdateNow(context.Background(), UI{Out: &out}, "dev", false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a refusal mentioning --force, got %v", err)
	}
}

func TestAssetNameMatchesGoreleaserNaming(t *testing.T) {
	name, err := assetName("1.2.3")
	if err != nil {
		t.Skipf("no published build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if !strings.HasPrefix(name, "envi_1.2.3_") {
		t.Fatalf("asset %q does not follow envi_<version>_<os>_<arch>", name)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".zip") {
		t.Fatalf("windows asset %q should be a .zip", name)
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(name, ".tar.gz") {
		t.Fatalf("asset %q should be a .tar.gz", name)
	}
}

func TestChecksumForPicksTheRightLine(t *testing.T) {
	sums := "aaa  envi_1.0.0_linux_amd64.tar.gz\nbbb  envi_1.0.0_darwin_arm64.tar.gz\n"
	if got := checksumFor(sums, "envi_1.0.0_darwin_arm64.tar.gz"); got != "bbb" {
		t.Fatalf("checksumFor = %q, want bbb", got)
	}
	if got := checksumFor(sums, "envi_1.0.0_windows_amd64.zip"); got != "" {
		t.Fatalf("checksumFor on a missing entry = %q, want empty", got)
	}
}

func TestCheckWritableRejectsUnwritableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := checkWritable(locked); err == nil {
		t.Fatal("expected an error for a read-only directory")
	}
	if err := checkWritable(dir); err != nil {
		t.Fatalf("writable directory rejected: %v", err)
	}
}

// The binary is executed and asked its version before the working one is moved
// aside; a mismatch or a binary that will not run must abort the update.
func TestVerifyBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in is not portable to windows")
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\necho 9.9.9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinary(context.Background(), good, "9.9.9"); err != nil {
		t.Fatalf("matching version rejected: %v", err)
	}
	if err := verifyBinary(context.Background(), good, "1.0.0"); err == nil {
		t.Fatal("expected a version mismatch to abort the update")
	}
	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinary(context.Background(), broken, "9.9.9"); err == nil {
		t.Fatal("expected a binary that will not run to abort the update")
	}
}
