package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type memoryStore struct{ tokens Tokens }

func (m *memoryStore) Save(v Tokens) error   { m.tokens = v; return nil }
func (m *memoryStore) Load() (Tokens, error) { return m.tokens, nil }
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
			w.Write([]byte(`{"access_token":"access","refresh_token":"refresh"}`))
		}
	}))
	defer s.Close()
	store := new(memoryStore)
	var out bytes.Buffer
	if e := Authenticate(context.Background(), Client{BaseURL: s.URL}, store, bytes.NewBufferString("123456\n"), &out, "a@b.com"); e != nil {
		t.Fatal(e)
	}
	if !requested || !verified || store.tokens.Access != "access" {
		t.Fatal("auth flow incomplete")
	}
}
func TestResolveEnvToken(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "service")
	v, e := ResolveToken(new(memoryStore))
	if e != nil || v != "service" {
		t.Fatal(v, e)
	}
}
