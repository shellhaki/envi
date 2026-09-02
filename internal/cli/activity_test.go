package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	projectctx "shellhaki/envi/internal/cli/project"
	"strings"
	"testing"
	"time"
)

func TestActivityFromContext(t *testing.T) {
	dir := t.TempDir()
	_ = projectctx.Write(dir, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p1", Name: "demo"}, Environment: projectctx.Resource{ID: "e1", Name: "dev"}})
	now := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects":
			w.Write([]byte(`[{"ID":"p1","OrgID":"org9","Name":"demo"}]`))
		case "/orgs/org9/audit-events":
			w.Write([]byte(`[{"action":"secret.write","target_type":"environment","target_id":"1a2b3c4d5e6f","actor":"dev@example.com","created_at":"` + now + `"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := Activity(context.Background(), Client{BaseURL: s.URL}, dir, 20, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"WHO", "secret.write", "dev@example.com", "environment 1a2b3c4d", "5m ago"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestActivityFallsBackToAccountOrg(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.Write([]byte(`{"OrganizationID":"orgP"}`))
		case "/orgs/orgP/audit-events":
			w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := Activity(context.Background(), Client{BaseURL: s.URL}, t.TempDir(), 20, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "No activity yet." {
		t.Fatalf("output %q", out.String())
	}
}

func TestActivityServiceTokenActor(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.Write([]byte(`{"OrganizationID":"orgP"}`))
		case "/orgs/orgP/audit-events":
			w.Write([]byte(`[{"action":"secret.read","target_type":"environment","target_id":"abcdef01","actor":"","created_at":"` + now + `"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := Activity(context.Background(), Client{BaseURL: s.URL}, t.TempDir(), 20, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "service token") {
		t.Fatalf("output %q", out.String())
	}
}
