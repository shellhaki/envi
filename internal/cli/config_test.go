package cli

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("ENVI_API_URL", "https://api.example.com")
	t.Setenv("ENVI_TOKEN", "token")
	t.Setenv("ENVI_CONFIG_DIR", t.TempDir())
	c := LoadConfig()
	if c.APIURL != "https://api.example.com" || c.Token != "token" {
		t.Fatalf("%#v", c)
	}
	if d, err := ConfigDir(); err != nil || d == "" {
		t.Fatal(err)
	}
}

// Which server the CLI talks to, by precedence. The released-binary case is
// the one that shipped broken once, pointing every install at localhost.
func TestAPIURLResolution(t *testing.T) {
	t.Run("released build with no env goes to production", func(t *testing.T) {
		t.Setenv("ENVI_API_URL", "")
		if got := APIURL("0.1.1"); got != DefaultAPIURL {
			t.Fatalf("released build resolved to %q, want %q", got, DefaultAPIURL)
		}
	})
	t.Run("production default is never a local address", func(t *testing.T) {
		if strings.Contains(DefaultAPIURL, "localhost") || strings.Contains(DefaultAPIURL, "127.0.0.1") {
			t.Fatalf("released CLI would default to a local address: %q", DefaultAPIURL)
		}
	})
	t.Run("unstamped build goes to the local server", func(t *testing.T) {
		t.Setenv("ENVI_API_URL", "")
		for _, v := range []string{"", "dev", "dev-abc123"} {
			if got := APIURL(v); got != DevAPIURL {
				t.Fatalf("version %q resolved to %q, want %q", v, got, DevAPIURL)
			}
		}
	})
	t.Run("ENVI_API_URL wins over both", func(t *testing.T) {
		t.Setenv("ENVI_API_URL", "https://staging.example.com")
		for _, v := range []string{"dev", "0.1.1"} {
			if got := APIURL(v); got != "https://staging.example.com" {
				t.Fatalf("version %q: env var ignored, got %q", v, got)
			}
		}
	})
	t.Run("whitespace-only env is treated as unset", func(t *testing.T) {
		t.Setenv("ENVI_API_URL", "   ")
		if got := APIURL("0.1.1"); got != DefaultAPIURL {
			t.Fatalf("got %q, want the production default", got)
		}
	})
}
