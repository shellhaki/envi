package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"shellhaki/envi/internal/cli/session"
	"strings"
	"time"
)

type Tokens struct {
	Access, Refresh string
	AccessExpiry    time.Time
}
type TokenStore interface {
	Save(Tokens) error
	Load() (Tokens, error)
	Clear() error
}
type sqliteStore struct{ store session.Store }

func (s sqliteStore) Save(t Tokens) error {
	return s.store.Save(session.Session{Access: t.Access, Refresh: t.Refresh, AccessExpiry: t.AccessExpiry})
}
func (s sqliteStore) Load() (Tokens, error) {
	v, err := s.store.Load()
	return Tokens{Access: v.Access, Refresh: v.Refresh, AccessExpiry: v.AccessExpiry}, err
}
func (s sqliteStore) Clear() error { return s.store.Clear() }

func defaultTokenStore() (TokenStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return sqliteStore{session.Store{Path: filepath.Join(dir, "session.db")}}, nil
}

type sessionResponse struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
}

func (r sessionResponse) tokens() Tokens {
	t := Tokens{Access: r.Access, Refresh: r.Refresh}
	if r.ExpiresIn > 0 {
		t.AccessExpiry = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return t
}

func Authenticate(ctx context.Context, c Client, store TokenStore, in io.Reader, out io.Writer, email string) error {
	r := bufio.NewReader(in)
	if email == "" {
		fmt.Fprint(out, "Email: ")
		v, e := r.ReadString('\n')
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		email = strings.TrimSpace(v)
	}
	if email == "" {
		return errors.New("email is required")
	}
	if e := c.Do(ctx, "POST", "/auth/request-otp", map[string]string{"email": email}, nil); e != nil {
		return e
	}
	fmt.Fprint(out, "Code: ")
	code, e := r.ReadString('\n')
	if e != nil && !errors.Is(e, io.EOF) {
		return e
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("OTP code is required")
	}
	var t sessionResponse
	if e = c.Do(ctx, "POST", "/auth/verify-otp", map[string]string{"email": email, "code": code}, &t); e != nil {
		return e
	}
	if t.Access == "" || t.Refresh == "" {
		return errors.New("invalid authentication response")
	}
	if e = store.Save(t.tokens()); e != nil {
		return fmt.Errorf("store credentials: %w", e)
	}
	fmt.Fprintln(out, "Authenticated")
	return nil
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// AuthenticateDevice runs the browser device flow: it asks the server for a code,
// shows the user where to enter it, then polls until the web session approves it.
// This is the default path; the CLI itself never sees the user's credentials.
func AuthenticateDevice(ctx context.Context, c Client, store TokenStore, out io.Writer, openBrowser bool) error {
	var d deviceCodeResponse
	if e := c.Do(ctx, "POST", "/auth/device/code", nil, &d); e != nil {
		return e
	}
	if d.DeviceCode == "" || d.UserCode == "" {
		return errors.New("invalid device authorization response")
	}
	target := d.VerificationURIComplete
	if target == "" {
		target = d.VerificationURI
	}
	fmt.Fprintf(out, "Your one-time code: %s\n", d.UserCode)
	if openBrowser {
		fmt.Fprintf(out, "Opening %s in your browser to approve it.\n", d.VerificationURI)
		if e := openURL(target); e != nil {
			fmt.Fprintf(out, "Couldn't open a browser. Visit %s and enter the code.\n", target)
		}
	} else {
		fmt.Fprintf(out, "Visit %s and enter the code.\n", target)
	}
	fmt.Fprintln(out, "Waiting for approval...")

	interval := time.Duration(d.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ttl := time.Duration(d.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	t, e := pollForToken(ctx, c, d.DeviceCode, interval, time.Now().Add(ttl), time.After)
	if e != nil {
		return e
	}
	if e = store.Save(t); e != nil {
		return fmt.Errorf("store credentials: %w", e)
	}
	fmt.Fprintln(out, "Authenticated")
	return nil
}

// pollForToken exchanges the device code for a session, retrying while the user
// has not yet approved. wait is injected so tests can advance without real sleeps.
func pollForToken(ctx context.Context, c Client, deviceCode string, interval time.Duration, deadline time.Time, wait func(time.Duration) <-chan time.Time) (Tokens, error) {
	for {
		select {
		case <-ctx.Done():
			return Tokens{}, ctx.Err()
		case <-wait(interval):
		}
		if time.Now().After(deadline) {
			return Tokens{}, errors.New("the code expired; run envi auth again")
		}
		var t sessionResponse
		e := c.Do(ctx, "POST", "/auth/device/token", map[string]string{"device_code": deviceCode}, &t)
		if e == nil {
			if t.Access == "" || t.Refresh == "" {
				return Tokens{}, errors.New("invalid authentication response")
			}
			return t.tokens(), nil
		}
		var api *APIError
		if !errors.As(e, &api) {
			return Tokens{}, e
		}
		switch api.Code {
		case "authorization_pending":
			// Not approved yet; keep polling.
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return Tokens{}, errors.New("authorization was denied")
		case "expired_token":
			return Tokens{}, errors.New("the code expired; run envi auth again")
		default:
			return Tokens{}, e
		}
	}
}

// Logout revokes the stored session upstream and clears it locally. The local
// session is cleared even if the server rejects the token, so a stale session
// cannot strand the user.
func Logout(ctx context.Context, c Client, store TokenStore, out io.Writer) error {
	t, err := store.Load()
	if err != nil || t.Refresh == "" {
		if e := store.Clear(); e != nil {
			return e
		}
		fmt.Fprintln(out, "Signed out")
		return nil
	}
	upstream := c.Do(ctx, "POST", "/auth/logout", map[string]string{"refresh_token": t.Refresh}, nil)
	if e := store.Clear(); e != nil {
		return e
	}
	if upstream != nil {
		fmt.Fprintln(out, "Signed out locally; the server rejected the session token")
		return nil
	}
	fmt.Fprintln(out, "Signed out")
	return nil
}

// authorize returns a Client that carries the stored access token and can rotate
// it. A static ENVI_TOKEN wins and is never refreshed: service tokens have no
// refresh counterpart.
func authorize(c Client, store TokenStore) (Client, error) {
	if v := LoadConfig().Token; v != "" {
		c.Token = v
		return c, nil
	}
	t, err := store.Load()
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return c, errors.New("not authenticated; run envi auth")
		}
		return c, err
	}
	if t.Access == "" {
		return c, errors.New("not authenticated; run envi auth")
	}
	c.Token = t.Access
	if t.Refresh == "" {
		return c, nil
	}
	// Refresh up front when the access token is known to be spent, so the command
	// does not burn a round trip discovering a 401.
	if !t.AccessExpiry.IsZero() && time.Now().After(t.AccessExpiry) {
		access, err := refresh(c, store, t.Refresh)
		if err != nil {
			return c, err
		}
		c.Token = access
		return c, nil
	}
	refreshToken := t.Refresh
	c.Refresh = func() (string, error) { return refresh(c, store, refreshToken) }
	return c, nil
}

// refresh exchanges a refresh token for a new pair and persists it. It uses a
// bare client so a failed refresh cannot recurse into another refresh.
func refresh(c Client, store TokenStore, refreshToken string) (string, error) {
	var t sessionResponse
	bare := Client{BaseURL: c.BaseURL, HTTP: c.HTTP}
	if err := bare.Do(context.Background(), "POST", "/auth/refresh", map[string]string{"refresh_token": refreshToken}, &t); err != nil {
		return "", errors.New("session expired; run envi auth")
	}
	if t.Access == "" || t.Refresh == "" {
		return "", errors.New("session expired; run envi auth")
	}
	if err := store.Save(t.tokens()); err != nil {
		return "", fmt.Errorf("store credentials: %w", err)
	}
	return t.Access, nil
}
