package cli

import "testing"

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
