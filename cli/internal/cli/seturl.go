package cli

// envi set-url: point this CLI at a self-hosted Envi instead of the hosted one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiURLFile = "api_url"

// savedAPIURL is the URL saved by envi set-url, or "".
func savedAPIURL() string {
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, apiURLFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// apiURLSource resolves the API URL and says where it came from.
func apiURLSource(version string) (string, string) {
	if v := strings.TrimSpace(os.Getenv("ENVI_API_URL")); v != "" {
		return v, "the ENVI_API_URL environment variable"
	}
	if v := savedAPIURL(); v != "" {
		return v, "envi set-url"
	}
	if IsDevBuild(version) {
		return DevAPIURL, "the default for a development build"
	}
	return DefaultAPIURL, "the default (hosted Envi)"
}

// NormalizeAPIURL checks a URL and returns it without a trailing slash.
func NormalizeAPIURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not an API URL like https://api.example.com", raw)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// checkHealth asks the server for /health and expects Envi's {"status":"ok"}.
func checkHealth(ctx context.Context, httpClient *http.Client, apiURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL+"/health", nil)
	if err != nil {
		return err
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var body struct{ Status string }
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body) != nil || body.Status != "ok" {
		return fmt.Errorf("%s/health answered %d, which is not an Envi API", apiURL, res.StatusCode)
	}
	return nil
}

type SetURLOptions struct {
	URL     string
	Force   bool // save even if the server doesn't answer
	Reset   bool // go back to the default
	Version string
	HTTP    *http.Client
}

// SetAPIURL saves (or with no URL, shows) the API URL. Changing server signs out,
// so a session from one server is never sent to another.
func SetAPIURL(ctx context.Context, o SetURLOptions, store TokenStore, out io.Writer) error {
	ui := NewUI(out)
	current, source := apiURLSource(o.Version)
	if o.URL == "" && !o.Reset {
		fmt.Fprintf(out, "%s\n%s\n", current, ui.Dim("from "+source))
		return nil
	}

	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, apiURLFile)

	next := ""
	if o.Reset {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if next, err = NormalizeAPIURL(o.URL); err != nil {
			return err
		}
		if !o.Force {
			httpClient := o.HTTP
			if httpClient == nil {
				httpClient = http.DefaultClient
			}
			if err := checkHealth(ctx, httpClient, next); err != nil {
				return fmt.Errorf("no Envi API answered at %s: %v\ncheck the URL, or save it anyway with: envi set-url %s --force", next, err, next)
			}
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(next+"\n"), 0o600); err != nil {
			return err
		}
	}

	resolved, _ := apiURLSource(o.Version)
	ui.Success("API URL: %s", resolved)
	if os.Getenv("ENVI_API_URL") != "" {
		ui.Warn("ENVI_API_URL is set, so this shell still uses it; unset it to use the saved URL.")
	}
	if strings.HasPrefix(resolved, "http://") && !isLocalHost(resolved) {
		ui.Warn("%s is plain http: your sign-in and secrets cross the network unencrypted. Put HTTPS in front of it.", resolved)
	}
	if resolved != current {
		if store != nil {
			if err := store.Clear(); err != nil {
				return fmt.Errorf("saved the URL, but couldn't sign out of %s: %w", current, err)
			}
		}
		ui.Print("Signed out of %s. Sign in to this server with: envi auth", current)
	}
	return nil
}

func isLocalHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}
