package cli

import (
	"os"
	"path/filepath"
)

type Config struct{ APIURL, Token string }

// DefaultAPIURL is the hosted service. A released binary has to reach Envi out
// of the box: defaulting to localhost only ever worked on a machine that
// happened to be running the API itself, and gave everyone else a connection
// refused on their first command. Self-hosters and local development point
// ENVI_API_URL somewhere else.
const DefaultAPIURL = "https://api.envisecrets.com"

func LoadConfig() Config {
	u := os.Getenv("ENVI_API_URL")
	if u == "" {
		u = DefaultAPIURL
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
