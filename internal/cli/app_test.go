package cli

import (
	"bytes"
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
