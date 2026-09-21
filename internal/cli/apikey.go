package cli

// Personal API keys, from the terminal.
//
//	envi auth --key envi_...      log in with a key instead of the browser
//	envi key create --name ci     make a key
//	envi key list                 show your keys
//	envi key revoke <id>          stop one working

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

type apiKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Permission string     `json:"permission"`
	Secret     string     `json:"secret"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// LoginWithKey trades an API key for a session and stores it, so the key itself
// never lands on disk. Nothing else about the session is special: it refreshes
// and expires like one made in a browser.
func LoginWithKey(ctx context.Context, c Client, store TokenStore, key string, out io.Writer) error {
	var session sessionResponse
	if err := c.Do(ctx, "POST", "/auth/api-key", map[string]string{"key": key}, &session); err != nil {
		return err
	}
	if session.Access == "" || session.Refresh == "" {
		return fmt.Errorf("the server did not return a session")
	}
	if err := store.Save(session.tokens()); err != nil {
		return fmt.Errorf("store credentials: %w", err)
	}
	fmt.Fprintln(out, "Authenticated")
	return nil
}

// CreateAPIKey makes a key and prints it. This is the only time it can be shown.
func CreateAPIKey(ctx context.Context, c Client, name, permission string, ttl time.Duration, out io.Writer) error {
	body := map[string]any{"name": name, "permission": permission}
	// Sent always, so "never expires" (0) is distinguishable from "not given".
	body["ttl_seconds"] = int64(ttl.Seconds())

	var key apiKey
	if err := c.Do(ctx, "POST", "/me/api-keys", body, &key); err != nil {
		return err
	}

	ui := NewUI(out)
	ui.Success("Created API key %s (%s)", ui.Bold(key.Name), key.Permission)
	fmt.Fprintln(out, key.Secret)
	ui.Warn("This is the only time the key is shown. Store it somewhere safe.")
	if key.ExpiresAt != nil {
		ui.Print("Expires %s.", key.ExpiresAt.Local().Format("2 Jan 2006"))
	} else {
		ui.Print("This key never expires.")
	}
	return nil
}

func ListAPIKeys(ctx context.Context, c Client, out io.Writer) error {
	var keys []apiKey
	if err := c.Do(ctx, "GET", "/me/api-keys", nil, &keys); err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Fprintln(out, "No API keys. Create one with: envi key create --name <name>")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPERMISSION\tEXPIRES\tLAST USED")
	for _, key := range keys {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			shortID(key.ID), key.Name, key.Permission, when(key.ExpiresAt, "never"), when(key.LastUsedAt, "never"))
	}
	return w.Flush()
}

func RevokeAPIKey(ctx context.Context, c Client, id string, out io.Writer) error {
	if err := c.Do(ctx, "DELETE", "/me/api-keys/"+id, nil, nil); err != nil {
		return err
	}
	NewUI(out).Success("Revoked. Any session started with that key is signed out.")
	return nil
}

// when formats an optional timestamp, with a word for "there isn't one".
func when(t *time.Time, absent string) string {
	if t == nil {
		return absent
	}
	if t.Before(time.Now()) {
		return "expired"
	}
	return t.Local().Format("2 Jan 2006")
}
