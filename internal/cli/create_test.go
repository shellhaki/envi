package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	projectctx "shellhaki/envi/internal/cli/project"
	"strings"
	"testing"
)

func TestCreateProject(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.Write([]byte(`{"ID":"u1","Email":"dev@example.com","OrganizationID":"org1"}`))
		case "/projects":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["org_id"] != "org1" || in["name"] != "acme" {
				t.Errorf("payload %#v", in)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"ID":"p1","OrgID":"org1","Name":"acme"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := CreateProject(context.Background(), Client{BaseURL: s.URL}, "acme", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Created project acme (p1)") {
		t.Fatalf("output %q", out.String())
	}
}

func TestCreateEnvWithProjectFlag(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects":
			w.Write([]byte(`[{"ID":"p1","OrgID":"org1","Name":"acme"}]`))
		case "/projects/p1/environments":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["name"] != "prod" || in["is_production"] != true {
				t.Errorf("payload %#v", in)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"ID":"e1","ProjectID":"p1","Name":"prod","Production":true}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := CreateEnv(context.Background(), Client{BaseURL: s.URL}, t.TempDir(), "acme", "prod", true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Created environment prod (e1) in acme [production]") {
		t.Fatalf("output %q", out.String())
	}
}

func TestCreateEnvFromContext(t *testing.T) {
	dir := t.TempDir()
	_ = projectctx.Write(dir, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p2", Name: "demo"}, Environment: projectctx.Resource{ID: "e0", Name: "dev"}})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			t.Errorf("should not list projects when using envi.toml context")
		}
		if r.URL.Path != "/projects/p2/environments" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"ID":"e2","ProjectID":"p2","Name":"staging","Production":false}`))
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := CreateEnv(context.Background(), Client{BaseURL: s.URL}, dir, "", "staging", false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Created environment staging (e2) in demo") || strings.Contains(out.String(), "[production]") {
		t.Fatalf("output %q", out.String())
	}
}
