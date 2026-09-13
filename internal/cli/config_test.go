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

// An unset ENVI_API_URL is the case every installed binary hits, and the one
// that shipped pointing at localhost.
func TestLoadConfigDefaultsToHostedAPI(t *testing.T) {
	t.Setenv("ENVI_API_URL", "")
	if got := LoadConfig().APIURL; got != DefaultAPIURL {
		t.Fatalf("default API URL = %q, want %q", got, DefaultAPIURL)
	}
	if strings.Contains(DefaultAPIURL, "localhost") || strings.Contains(DefaultAPIURL, "127.0.0.1") {
		t.Fatalf("released CLI would default to a local address: %q", DefaultAPIURL)
	}
}
