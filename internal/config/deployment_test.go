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

// Values typed with quotes into a hosting panel must still work. Before this,
// ENVI_MODE="hosted" silently left billing off.
func TestQuotedSettings(t *testing.T) {
	for _, typed := range []string{`hosted`, `"hosted"`, `'hosted'`, `  "hosted"  `, ` hosted `} {
		t.Setenv("ENVI_MODE", typed)
		if d := LoadDeployment(filepath.Join(t.TempDir(), "missing.json")); d.Mode != Hosted {
			t.Errorf("ENVI_MODE=%s: got %s", typed, d.Mode)
		}
	}
	t.Setenv("X_KEY", `"sk_live_abc"`)
	if got := Setting("X_KEY"); got != "sk_live_abc" {
		t.Errorf("quoted key read as %q", got)
	}
	t.Setenv("X_KEY", `"unbalanced`)
	if got := Setting("X_KEY"); got != `"unbalanced` {
		t.Errorf("an unbalanced quote was stripped: %q", got)
	}
}
