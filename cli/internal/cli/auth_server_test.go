package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAskServer(t *testing.T) {
	cases := []struct {
		name, saved, input string
		cloud              bool
		url, err           string
	}{
		{name: "Enter keeps Envi Cloud", input: "\n", cloud: true},
		{name: "an instance URL", input: "2\nhttps://api.envi.example.com\n", url: "https://api.envi.example.com"},
		{name: "Enter keeps the saved instance", saved: "https://api.mine.dev", input: "\n\n", url: "https://api.mine.dev"},
		{name: "back to Envi Cloud", saved: "https://api.mine.dev", input: "1\n", cloud: true},
		{name: "no URL for a new instance", input: "2\n\n", err: "instance API URL is needed"},
		{name: "not a choice", input: "3\n", err: `"3" isn't 1 or 2`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			cloud, url, err := askServer(strings.NewReader(c.input), &out, c.saved)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil || cloud != c.cloud || url != c.url {
				t.Fatalf("got cloud=%v url=%q err=%v", cloud, url, err)
			}
			if !strings.Contains(out.String(), "Envi Cloud") {
				t.Fatalf("question doesn't offer Envi Cloud: %q", out.String())
			}
		})
	}
}

func TestChooseServerFlags(t *testing.T) {
	freshConfig(t)
	s := enviServer(t)
	store := &memoryStore{tokens: Tokens{Access: "cloud-token"}}
	var out bytes.Buffer
	app := App{Out: &out, Err: &out, Version: "1.0.0"}

	if err := app.chooseServer(store, false, s.URL, nil); err != nil {
		t.Fatal(err)
	}
	if APIURL("1.0.0") != s.URL || store.tokens.Access != "" {
		t.Fatalf("--url: API %q, token %q", APIURL("1.0.0"), store.tokens.Access)
	}
	if strings.Contains(out.String(), "Sign in to this server with") {
		t.Fatalf("envi auth is already signing in: %q", out.String())
	}

	store.tokens.Access = "instance-token"
	if err := app.chooseServer(store, false, s.URL+"/", nil); err != nil || store.tokens.Access != "instance-token" {
		t.Fatalf("the same instance again signed out (err %v)", err)
	}

	if err := app.chooseServer(store, true, "", nil); err != nil {
		t.Fatal(err)
	}
	if APIURL("1.0.0") != DefaultAPIURL || store.tokens.Access != "" {
		t.Fatalf("--cloud: API %q, token %q", APIURL("1.0.0"), store.tokens.Access)
	}

	if err := app.chooseServer(store, true, s.URL, nil); err == nil {
		t.Fatal("--cloud and --url together were accepted")
	}
	t.Setenv("ENVI_API_URL", s.URL)
	if err := app.chooseServer(store, true, "", nil); err == nil {
		t.Fatal("--cloud was accepted while ENVI_API_URL decides the server")
	}
}

func TestChooseServerDoesNotAskWithoutATerminal(t *testing.T) {
	freshConfig(t)
	var out bytes.Buffer
	app := App{In: strings.NewReader("2\nhttps://x.example\n"), Out: &out, Err: &out, Version: "1.0.0"}
	if err := app.chooseServer(&memoryStore{}, false, "", app.In); err != nil || out.Len() != 0 {
		t.Fatalf("err %v, printed %q", err, out.String())
	}
}
