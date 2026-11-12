package cli

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct{ APIURL, Token string }

// DefaultAPIURL is the hosted service.
var DefaultAPIURL = "https://api.envisecrets.com"

// DevAPIURL is where a binary built straight from source looks.
const DevAPIURL = "http://127.0.0.1:8080"

// IsDevBuild reports a binary built with go build or go run, not a release.
func IsDevBuild(version string) bool {
	v := strings.TrimSpace(version)
	return v == "" || v == "dev" || strings.HasPrefix(v, "dev-")
}

// APIURL resolves which server this CLI talks to, most explicit first:
// ENVI_API_URL, then the URL saved by envi set-url, then the built-in default.
func APIURL(version string) string {
	v, _ := apiURLSource(version)
	return v
}

func LoadConfig() Config {
	return Config{APIURL(""), os.Getenv("ENVI_TOKEN")}
}

func ConfigDir() (string, error) {
	if v := os.Getenv("ENVI_CONFIG_DIR"); v != "" {
		return v, nil
	}
	d, e := os.UserConfigDir()
	return filepath.Join(d, "envi"), e
}
