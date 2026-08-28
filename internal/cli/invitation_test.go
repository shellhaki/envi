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

func TestShareAndAcceptInvitation(t *testing.T) {
	dir := t.TempDir()
	_ = projectctx.Write(dir, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}})
	var accepted bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch r.URL.Path {
		case "/projects/p/invitations":
			if in["email"] != "guest@example.com" || in["environment_id"] != "e" || in["permission"] != "write" {
				t.Errorf("payload %#v", in)
			}
			w.Write([]byte(`{"Token":"invite-token"}`))
		case "/invitations/accept":
			accepted = in["token"] == "invite-token"
			w.WriteHeader(204)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	c := Client{BaseURL: s.URL}
	if err := Share(context.Background(), c, dir, "guest@example.com", "demo", "dev", "write", &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "invite-token" {
		t.Fatal(out.String())
	}
	if err := AcceptInvitation(context.Background(), c, "invite-token"); err != nil || !accepted {
		t.Fatal(err)
	}
}
