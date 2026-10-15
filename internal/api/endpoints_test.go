package api

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"testing"

	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
)

// The router is the truth. ENDPOINTS.md and excalidraw.json are written by
// hand and by scripts/diagram.py, and this fails the moment either one stops
// matching it, so a new endpoint can't ship undocumented.
func TestEndpointsAreDocumented(t *testing.T) {
	r := Build(nil, auth.LoginSettings{}, project.Service{}, secret.Service{}, kv.Store{}, audit.Service{},
		service_token.Service{}, invitation.Service{}, "", false, config.Deployment{}, announce.Service{}, billing.Service{})
	var routes []string
	for _, route := range r.Routes() {
		routes = append(routes, route.Method+" "+route.Path)
	}
	sort.Strings(routes)

	raw, err := os.ReadFile("../../ENDPOINTS.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, m := range regexp.MustCompile("(?m)^\\| (GET|POST|PUT|PATCH|DELETE) \\| `([^`]+)`").FindAllStringSubmatch(string(raw), -1) {
		documented = append(documented, m[1]+" "+m[2])
	}
	sort.Strings(documented)
	compare(t, "ENDPOINTS.md", routes, documented)

	raw, err = os.ReadFile("../../excalidraw.json")
	if err != nil {
		t.Fatal(err)
	}
	var drawing struct {
		Elements []struct {
			Text string `json:"text"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &drawing); err != nil {
		t.Fatal(err)
	}
	var drawn []string
	row := regexp.MustCompile(`(?m)^(GET|POST|PUT|PATCH|DELETE) +(\S+)`)
	for _, e := range drawing.Elements {
		for _, m := range row.FindAllStringSubmatch(e.Text, -1) {
			drawn = append(drawn, m[1]+" "+m[2])
		}
	}
	sort.Strings(drawn)
	compare(t, "excalidraw.json (run make diagram)", routes, drawn)
}

func compare(t *testing.T, name string, want, got []string) {
	t.Helper()
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	served := map[string]bool{}
	for _, w := range want {
		served[w] = true
		if !have[w] {
			t.Errorf("%s is missing %s", name, w)
		}
	}
	for _, g := range got {
		if !served[g] {
			t.Errorf("%s lists %s, which the server does not register", name, g)
		}
	}
}

// The data model half of excalidraw.json is drawn from schema.sql; a table
// added there and not drawn means make diagram was not run.
func TestDiagramShowsEveryTable(t *testing.T) {
	schema, err := os.ReadFile("../../migrations/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../excalidraw.json")
	if err != nil {
		t.Fatal(err)
	}
	var drawing struct {
		Elements []struct {
			Text string `json:"text"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &drawing); err != nil {
		t.Fatal(err)
	}
	drawn := map[string]bool{}
	for _, e := range drawing.Elements {
		drawn[e.Text] = true
	}
	for _, m := range regexp.MustCompile(`CREATE TABLE (\w+)`).FindAllStringSubmatch(string(schema), -1) {
		if !drawn[m[1]] {
			t.Errorf("excalidraw.json has no %s table (run make diagram)", m[1])
		}
	}
}

// The services section is drawn from the Go packages; a new package that is
// not drawn means make diagram was not run.
func TestDiagramShowsEveryService(t *testing.T) {
	raw, err := os.ReadFile("../../excalidraw.json")
	if err != nil {
		t.Fatal(err)
	}
	var drawing struct {
		Elements []struct {
			Text string `json:"text"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &drawing); err != nil {
		t.Fatal(err)
	}
	drawn := map[string]bool{}
	for _, e := range drawing.Elements {
		drawn[e.Text] = true
	}
	for _, dir := range []string{"../../internal", "../../cmd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || name == "e2e" {
				continue
			}
			if dir == "../../cmd" {
				name = "cmd/" + name
			}
			if !drawn[name] {
				t.Errorf("excalidraw.json has no %s service (run make diagram)", name)
			}
		}
	}
}
