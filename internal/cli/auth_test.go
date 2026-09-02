package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type memoryStore struct {
	tokens  Tokens
	cleared bool
}

func (m *memoryStore) Save(v Tokens) error   { m.tokens = v; return nil }
func (m *memoryStore) Load() (Tokens, error) { return m.tokens, nil }
func (m *memoryStore) Clear() error          { m.tokens = Tokens{}; m.cleared = true; return nil }

func TestAuthenticate(t *testing.T) {
	var requested, verified bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch r.URL.Path {
		case "/auth/request-otp":
			requested = in["email"] == "a@b.com"
			w.WriteHeader(202)
		case "/auth/verify-otp":
			verified = in["code"] == "123456"
			w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":900}`))
		}
	}))
	defer s.Close()
	store := new(memoryStore)
	var out bytes.Buffer
	if e := Authenticate(context.Background(), Client{BaseURL: s.URL}, store, bytes.NewBufferString("123456\n"), &out, "a@b.com"); e != nil {
		t.Fatal(e)
	}
	if !requested || !verified || store.tokens.Access != "access" || store.tokens.Refresh != "refresh" {
		t.Fatal("auth flow incomplete")
	}
	// expires_in must be recorded, or the CLI cannot refresh ahead of expiry.
	if store.tokens.AccessExpiry.IsZero() || time.Until(store.tokens.AccessExpiry) > 15*time.Minute {
		t.Fatalf("access expiry = %v", store.tokens.AccessExpiry)
	}
}

func TestStaticTokenWinsAndIsNotRefreshed(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "service")
	c, err := authorize(Client{BaseURL: "http://example.invalid"}, new(memoryStore))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "service" {
		t.Fatalf("token = %q", c.Token)
	}
	// Service tokens have no refresh counterpart; attempting one would 401.
	if c.Refresh != nil {
		t.Fatal("static token wired up a refresh hook")
	}
}

func TestUnauthenticatedStore(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "")
	if _, err := authorize(Client{}, new(memoryStore)); err == nil {
		t.Fatal("empty store accepted")
	}
}

// A CLI command must survive an expired access token by rotating it, rather than
// failing and forcing the user to run envi auth again.
func TestRefreshesOnUnauthorized(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "")
	var refreshed int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/refresh":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["refresh_token"] != "refresh" {
				w.WriteHeader(401)
				return
			}
			refreshed++
			w.Write([]byte(`{"access_token":"fresh","refresh_token":"rotated","expires_in":900}`))
		case "/projects":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(401)
				w.Write([]byte(`{"code":"unauthenticated","error":"authentication required"}`))
				return
			}
			w.Write([]byte(`[{"ID":"p1"}]`))
		}
	}))
	defer s.Close()
	store := &memoryStore{tokens: Tokens{Access: "stale", Refresh: "refresh"}}
	c, err := authorize(Client{BaseURL: s.URL}, store)
	if err != nil {
		t.Fatal(err)
	}
	var out []struct{ ID string }
	if err = c.Do(context.Background(), "GET", "/projects", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "p1" {
		t.Fatalf("response = %+v", out)
	}
	if refreshed != 1 {
		t.Fatalf("refreshed %d times", refreshed)
	}
	// The rotated pair must be persisted, or the next command refreshes a token
	// the server has already revoked.
	if store.tokens.Access != "fresh" || store.tokens.Refresh != "rotated" {
		t.Fatalf("stored %+v", store.tokens)
	}
}

func TestRefreshesBeforeExpiry(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "")
	var refreshed int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshed++
		w.Write([]byte(`{"access_token":"fresh","refresh_token":"rotated","expires_in":900}`))
	}))
	defer s.Close()
	store := &memoryStore{tokens: Tokens{Access: "stale", Refresh: "refresh", AccessExpiry: time.Now().Add(-time.Minute)}}
	c, err := authorize(Client{BaseURL: s.URL}, store)
	if err != nil {
		t.Fatal(err)
	}
	// A known-expired token is rotated up front, without spending a 401.
	if c.Token != "fresh" || refreshed != 1 {
		t.Fatalf("token = %q refreshed = %d", c.Token, refreshed)
	}
}

func TestExpiredRefreshTokenAsksForReauth(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid refresh token"}`))
	}))
	defer s.Close()
	store := &memoryStore{tokens: Tokens{Access: "stale", Refresh: "dead", AccessExpiry: time.Now().Add(-time.Minute)}}
	_, err := authorize(Client{BaseURL: s.URL}, store)
	if err == nil || err.Error() != "session expired; run envi auth" {
		t.Fatalf("err = %v", err)
	}
}

func TestLogoutRevokesAndClears(t *testing.T) {
	var revoked string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		revoked = in["refresh_token"]
		w.WriteHeader(204)
	}))
	defer s.Close()
	store := &memoryStore{tokens: Tokens{Access: "access", Refresh: "refresh"}}
	var out bytes.Buffer
	if err := Logout(context.Background(), Client{BaseURL: s.URL}, store, &out); err != nil {
		t.Fatal(err)
	}
	if revoked != "refresh" || !store.cleared {
		t.Fatalf("revoked = %q cleared = %v", revoked, store.cleared)
	}
}

// A server that rejects the token must still leave the machine signed out.
func TestLogoutClearsWhenServerRejects(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid refresh token"}`))
	}))
	defer s.Close()
	store := &memoryStore{tokens: Tokens{Access: "access", Refresh: "refresh"}}
	var out bytes.Buffer
	if err := Logout(context.Background(), Client{BaseURL: s.URL}, store, &out); err != nil {
		t.Fatal(err)
	}
	if !store.cleared {
		t.Fatal("local session survived a rejected logout")
	}
}

// instantWait fires immediately so device-poll tests never sleep for real.
func instantWait(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func TestPollForTokenApproves(t *testing.T) {
	var calls int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"authorization_pending","error":"waiting"}`))
			return
		}
		w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":900}`))
	}))
	defer s.Close()
	tok, err := pollForToken(context.Background(), Client{BaseURL: s.URL}, "dev", time.Millisecond, time.Now().Add(time.Minute), instantWait)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a" || tok.Refresh != "r" {
		t.Fatalf("tokens = %+v", tok)
	}
	if tok.AccessExpiry.IsZero() {
		t.Fatal("access expiry not recorded")
	}
	if calls != 3 {
		t.Fatalf("polled %d times, want 3", calls)
	}
}

func TestPollForTokenDenied(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"access_denied","error":"denied"}`))
	}))
	defer s.Close()
	_, err := pollForToken(context.Background(), Client{BaseURL: s.URL}, "dev", time.Millisecond, time.Now().Add(time.Minute), instantWait)
	if err == nil || err.Error() != "authorization was denied" {
		t.Fatalf("err = %v, want denied", err)
	}
}

func TestPollForTokenExpiredByServer(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"expired_token","error":"expired"}`))
	}))
	defer s.Close()
	_, err := pollForToken(context.Background(), Client{BaseURL: s.URL}, "dev", time.Millisecond, time.Now().Add(time.Minute), instantWait)
	if err == nil || err.Error() != "the code expired; run envi auth again" {
		t.Fatalf("err = %v, want expired", err)
	}
}

// The local deadline stops polling even if the server never says expired.
func TestPollForTokenLocalDeadline(t *testing.T) {
	_, err := pollForToken(context.Background(), Client{BaseURL: "http://example.invalid"}, "dev", time.Millisecond, time.Now().Add(-time.Second), instantWait)
	if err == nil || err.Error() != "the code expired; run envi auth again" {
		t.Fatalf("err = %v, want expired", err)
	}
}

// slow_down must widen the poll interval by 5s, per RFC 8628.
func TestPollForTokenSlowDown(t *testing.T) {
	var calls int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"slow_down","error":"too fast"}`))
			return
		}
		w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":900}`))
	}))
	defer s.Close()
	var waits []time.Duration
	wait := func(d time.Duration) <-chan time.Time {
		waits = append(waits, d)
		return instantWait(d)
	}
	if _, err := pollForToken(context.Background(), Client{BaseURL: s.URL}, "dev", 5*time.Second, time.Now().Add(time.Minute), wait); err != nil {
		t.Fatal(err)
	}
	if len(waits) < 2 || waits[1] != 10*time.Second {
		t.Fatalf("wait intervals = %v, want second wait 10s", waits)
	}
}
