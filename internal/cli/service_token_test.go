package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	projectctx "shellhaki/envi/internal/cli/project"
	"strings"
	"testing"
)

func TestCreateServiceToken(t *testing.T) {
	d := t.TempDir()
	_ = projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "prod"}})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/p/environments/e/service-tokens" {
			t.Error(r.URL.Path)
		}
		w.Write([]byte(`{"Value":"once-only"}`))
	}))
	defer s.Close()
	var out bytes.Buffer
	if e := CreateServiceToken(context.Background(), Client{BaseURL: s.URL}, d, "deploy", "read", 60, &out); e != nil {
		t.Fatal(e)
	}
	if strings.TrimSpace(out.String()) != "once-only" {
		t.Fatal(out.String())
	}
}
