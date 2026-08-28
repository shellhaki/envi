package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectctx "shellhaki/envi/internal/cli/project"
)

func TestPullPush(t *testing.T) {
	d := t.TempDir()
	if e := projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}}); e != nil {
		t.Fatal(e)
	}
	var pushed string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing auth")
		}
		if r.Method == "GET" {
			w.Write([]byte(`{"values":{"B":"2","A":"1"},"revision":4}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		pushed = string(b)
		w.Write([]byte(`{"revision":5}`))
	}))
	defer s.Close()
	c := Client{BaseURL: s.URL, Token: "token"}
	if e := Pull(context.Background(), c, d); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(d, ".env"))
	if e != nil || string(b) != "A=1\nB=2\n" {
		t.Fatalf("%q %v", b, e)
	}
	info, _ := os.Stat(filepath.Join(d, ".env"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if e = Push(context.Background(), c, d); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(pushed, `"A":"1"`) || !strings.Contains(pushed, `"B":"2"`) {
		t.Fatal(pushed)
	}
	x, _ := projectctx.Load(d)
	if x.Environment.Revision != 5 || !strings.Contains(pushed, `"expected_revision":4`) {
		t.Fatal("revision not advanced", pushed)
	}
}

func TestDiff(t *testing.T) {
	d := t.TempDir()
	_ = projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}})
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("ADDED=1\nCHANGED=local\nSAME=x\n"), 0600)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"values":{"CHANGED":"remote","REMOVED":"1","SAME":"x"},"revision":2}`))
	}))
	defer s.Close()
	var out strings.Builder
	if e := Diff(context.Background(), Client{BaseURL: s.URL}, d, &out); e != nil {
		t.Fatal(e)
	}
	if out.String() != "added ADDED\nchanged CHANGED\nremoved REMOVED\n" {
		t.Fatal(out.String())
	}
}
func TestPushErrors(t *testing.T) {
	d := t.TempDir()
	if e := Push(context.Background(), Client{}, d); e == nil {
		t.Fatal("missing config accepted")
	}
	_ = projectctx.Write(d, projectctx.Context{Version: 1, Project: projectctx.Resource{ID: "p", Name: "demo"}, Environment: projectctx.Resource{ID: "e", Name: "dev"}})
	_ = os.WriteFile(filepath.Join(d, ".env"), []byte("bad"), 0600)
	if e := Push(context.Background(), Client{}, d); e == nil {
		t.Fatal("malformed env accepted")
	}
}
