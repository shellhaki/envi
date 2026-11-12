package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func enviServer(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func freshConfig(t *testing.T) {
	t.Setenv("ENVI_CONFIG_DIR", t.TempDir())
	t.Setenv("ENVI_API_URL", "")
}

func TestSetURLSavesAndSignsOut(t *testing.T) {
	freshConfig(t)
	s := enviServer(t)
	store := &memoryStore{tokens: Tokens{Access: "hosted-token"}}
	var out bytes.Buffer

	if err := SetAPIURL(context.Background(), SetURLOptions{URL: s.URL + "/", Version: "1.0.0"}, store, &out); err != nil {
		t.Fatal(err)
	}
	if got := APIURL("1.0.0"); got != s.URL {
		t.Fatalf("APIURL = %q, want %q", got, s.URL)
	}
	if !store.cleared {
		t.Fatal("changing server kept the old session, which would send it to the new server")
	}
	if !strings.Contains(out.String(), "Signed out of "+DefaultAPIURL) {
		t.Fatal(out.String())
	}

	store.cleared = false
	if err := SetAPIURL(context.Background(), SetURLOptions{URL: s.URL, Version: "1.0.0"}, store, &out); err != nil {
		t.Fatal(err)
	}
	if store.cleared {
		t.Fatal("setting the same URL again signed out")
	}
}

func TestSetURLRefusesWhatIsNotEnvi(t *testing.T) {
	freshConfig(t)
	notEnvi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }))
	defer notEnvi.Close()
	store := &memoryStore{}
	var out bytes.Buffer

	err := SetAPIURL(context.Background(), SetURLOptions{URL: notEnvi.URL, Version: "1.0.0"}, store, &out)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if APIURL("1.0.0") != DefaultAPIURL || store.cleared {
		t.Fatal("a refused URL was saved")
	}

	if err := SetAPIURL(context.Background(), SetURLOptions{URL: notEnvi.URL, Force: true, Version: "1.0.0"}, store, &out); err != nil {
		t.Fatal(err)
	}
	if APIURL("1.0.0") != notEnvi.URL {
		t.Fatal("--force did not save")
	}
}

func TestSetURLResetAndPrecedence(t *testing.T) {
	freshConfig(t)
	s := enviServer(t)
	var out bytes.Buffer
	if err := SetAPIURL(context.Background(), SetURLOptions{URL: s.URL, Version: "dev"}, &memoryStore{}, &out); err != nil {
		t.Fatal(err)
	}
	if APIURL("dev") != s.URL {
		t.Fatal("a saved URL should win over the development default")
	}
	t.Setenv("ENVI_API_URL", "https://env.example.com")
	if APIURL("1.0.0") != "https://env.example.com" {
		t.Fatal("ENVI_API_URL should win over the saved URL")
	}
	t.Setenv("ENVI_API_URL", "")

	if err := SetAPIURL(context.Background(), SetURLOptions{Reset: true, Version: "1.0.0"}, &memoryStore{}, &out); err != nil {
		t.Fatal(err)
	}
	if APIURL("1.0.0") != DefaultAPIURL {
		t.Fatal("--reset did not go back to the default")
	}

	out.Reset()
	if err := SetAPIURL(context.Background(), SetURLOptions{Version: "1.0.0"}, nil, &out); err != nil || !strings.Contains(out.String(), DefaultAPIURL) {
		t.Fatalf("showing the URL: %v %q", err, out.String())
	}
}

func TestNormalizeAPIURL(t *testing.T) {
	good := map[string]string{
		"https://api.example.com/":     "https://api.example.com",
		" http://10.0.0.5:8080 ":       "http://10.0.0.5:8080",
		"https://example.com/envi/api": "https://example.com/envi/api",
	}
	for in, want := range good {
		if got, err := NormalizeAPIURL(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "api.example.com", "ftp://x", "https://", "https://x?y=1"} {
		if _, err := NormalizeAPIURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSetURLCommand(t *testing.T) {
	freshConfig(t)
	s := enviServer(t)
	var out, errOut bytes.Buffer
	a := App{Out: &out, Err: &errOut, Version: "1.0.0", Store: &memoryStore{}}
	if code := a.Run([]string{"set-url", s.URL}); code != ExitOK {
		t.Fatal(errOut.String())
	}
	if code := a.Run([]string{"set-url", "nope"}); code != ExitUsage {
		t.Fatal("bad URL accepted")
	}
	out.Reset()
	a.Run([]string{"set-url"})
	if !strings.Contains(out.String(), s.URL) || !strings.Contains(out.String(), "envi set-url") {
		t.Fatal(out.String())
	}
}
