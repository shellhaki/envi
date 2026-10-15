package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentDefaultsToSelfHosted(t *testing.T) {
	t.Setenv("ENVI_MODE", "")
	if d := LoadDeployment(filepath.Join(t.TempDir(), "missing.json")); d.Mode != SelfHosted || d.Billing() {
		t.Fatalf("no file and no variable: %+v", d)
	}
}

// A container has no config file, so the variable alone must be enough.
func TestEnvironmentAloneTurnsHostedOn(t *testing.T) {
	t.Setenv("ENVI_MODE", "hosted")
	if d := LoadDeployment(filepath.Join(t.TempDir(), "missing.json")); d.Mode != Hosted || !d.Billing() {
		t.Fatalf("ENVI_MODE=hosted with no file: %+v", d)
	}
}

func TestEnvironmentBeatsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envi.config.json")
	os.WriteFile(path, []byte(`{"mode":"hosted","currency":"NGN"}`), 0o644)
	t.Setenv("ENVI_MODE", "self-hosted")
	t.Setenv("ENVI_CURRENCY", "USD")
	if d := LoadDeployment(path); d.Mode != SelfHosted || d.Currency != "USD" {
		t.Fatalf("variables did not win: %+v", d)
	}
}

// Nonsense in either place falls back to the safe default rather than
// switching billing on.
func TestNonsenseIsSelfHosted(t *testing.T) {
	t.Setenv("ENVI_MODE", "production")
	if d := LoadDeployment(filepath.Join(t.TempDir(), "missing.json")); d.Mode != SelfHosted {
		t.Fatalf("ENVI_MODE=production: %+v", d)
	}
}
