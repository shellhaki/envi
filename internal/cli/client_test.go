package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing token")
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer s.Close()
	var out struct {
		OK bool `json:"ok"`
	}
	if e := (Client{BaseURL: s.URL, Token: "token"}).Do(context.Background(), "GET", "/test", nil, &out); e != nil || !out.OK {
		t.Fatal(e)
	}
}
func TestAPIError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"code":"unauthenticated","error":"authentication required"}`))
	}))
	defer s.Close()
	e := (Client{BaseURL: s.URL}).Do(context.Background(), "GET", "/", nil, nil)
	var api *APIError
	if !errors.As(e, &api) || ExitCode(e) != ExitAuth {
		t.Fatalf("%T %v", e, e)
	}
}
