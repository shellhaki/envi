package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestApp(t *testing.T) {
	var out, err bytes.Buffer
	a := App{Out: &out, Err: &err, Version: "1.0.0"}
	if a.Run([]string{"version"}) != 0 || strings.TrimSpace(out.String()) != "1.0.0" {
		t.Fatal(out.String())
	}
	if a.Run([]string{"bad"}) != ExitUsage || !strings.Contains(err.String(), "unknown command") {
		t.Fatal(err.String())
	}
}

func TestInitRequiresAuth(t *testing.T) {
	t.Setenv("ENVI_TOKEN", "")
	var out, err bytes.Buffer
	a := App{In: bytes.NewBuffer(nil), Out: &out, Err: &err, Store: new(memoryStore)}
	if a.Run([]string{"init", "--project", "demo"}) != ExitAuth {
		t.Fatal(err.String())
	}
}

// The shapes a user actually types. The file is positional and optional, so the
// parser has to tell ".env.test" from "origin" from a flag.
func TestParsePushArgs(t *testing.T) {
	cases := []struct {
		args         []string
		file, origin string
		rest         []string
	}{
		{[]string{}, "", "", []string{}},
		{[]string{".env.test"}, ".env.test", "", []string{}},
		{[]string{"--force"}, "", "", []string{"--force"}},
		{[]string{"origin", "dev"}, "", "dev", []string{}},
		{[]string{".env.test", "origin", "dev"}, ".env.test", "dev", []string{}},
		{[]string{".env.test", "origin", "dev", "--force"}, ".env.test", "dev", []string{"--force"}},
		{[]string{".env.test", "--force"}, ".env.test", "", []string{"--force"}},
		{[]string{"origin", "dev", "--yes"}, "", "dev", []string{"--yes"}},
	}
	for _, c := range cases {
		file, origin, rest, err := parsePushArgs(c.args)
		if err != nil {
			t.Errorf("push %v: %v", c.args, err)
			continue
		}
		if file != c.file || origin != c.origin || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("push %v -> file=%q origin=%q rest=%v, want file=%q origin=%q rest=%v",
				c.args, file, origin, rest, c.file, c.origin, c.rest)
		}
	}
	for _, args := range [][]string{{"origin"}, {".env.test", "origin"}, {"origin", "--force"}} {
		if _, _, _, err := parsePushArgs(args); err == nil {
			t.Errorf("push %v was accepted with no origin name", args)
		}
	}
}
