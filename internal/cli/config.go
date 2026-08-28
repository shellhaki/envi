package cli

import (
	"os"
	"path/filepath"
)

type Config struct{ APIURL, Token string }

func LoadConfig() Config {
	u := os.Getenv("ENVI_API_URL")
	if u == "" {
		u = "http://127.0.0.1:8080"
	}
	return Config{u, os.Getenv("ENVI_TOKEN")}
}
func ConfigDir() (string, error) {
	if v := os.Getenv("ENVI_CONFIG_DIR"); v != "" {
		return v, nil
	}
	d, e := os.UserConfigDir()
	return filepath.Join(d, "envi"), e
}
