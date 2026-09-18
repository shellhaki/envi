package cli

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct{ APIURL, Token string }

// DefaultAPIURL is the hosted service, and the fallback for anything that is
// not obviously a development build. It is a var rather than a const so a
// build can point elsewhere — a staging channel, say — with:
//
//	go build -ldflags "-X shellhaki/envi/internal/cli.DefaultAPIURL=https://staging.example.com"
//
// The production URL is deliberately the fallback rather than something
// injected by CI. If it were injected and the injection ever silently missed,
// every released binary would point somewhere unreachable; defaulting to
// production means a failed injection still produces a working CLI.
var DefaultAPIURL = "https://api.envisecrets.com"

// DevAPIURL is where a binary built straight from source looks. Local
// development is the case that can afford to be wrong: whoever built it is
// standing right there.
const DevAPIURL = "http://127.0.0.1:8080"

// IsDevBuild reports a binary built without a stamped version — `go build` or
// `go run` rather than a release. GoReleaser sets main.version on every
// release, so a released binary is never mistaken for this.
func IsDevBuild(version string) bool {
	v := strings.TrimSpace(version)
	return v == "" || v == "dev" || strings.HasPrefix(v, "dev-")
}

// APIURL resolves which server this CLI talks to, most explicit first:
// ENVI_API_URL, then the development default for unstamped builds, then the
// hosted service.
func APIURL(version string) string {
	if v := strings.TrimSpace(os.Getenv("ENVI_API_URL")); v != "" {
		return v
	}
	if IsDevBuild(version) {
		return DevAPIURL
	}
	return DefaultAPIURL
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
