package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capture is the sink the real command fills with the clipboard; here it just
// keeps the text so the test can read what would have been pasted.
func capture(into *string) func(string) error {
	return func(text string) error { *into = text; return nil }
}

// The text handed out is a .env file: sorted KEY=VALUE lines, exactly what
// envi pull writes, because a platform parsing one must parse the other.
func TestCopyRendersSortedDotenv(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{"PORT": "8080", "API_KEY": "abc"}, &sent)}

	var copied string
	count, label, err := Copy(context.Background(), c, d, "", capture(&copied))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("copied %d secrets, want 2", count)
	}
	if copied != "API_KEY=abc\nPORT=8080\n" {
		t.Fatalf("clipboard text is %q", copied)
	}
	// The label names where they came from, so a paste into the wrong
	// platform screen is at least visible in the terminal afterwards.
	if !strings.Contains(label, "demo") || !strings.Contains(label, "dev") {
		t.Fatalf("label is %q, want the project and origin", label)
	}
}

// A value with a newline has to come back quoted, or every platform parsing the
// paste sees a truncated secret followed by a line of garbage.
func TestCopyQuotesValuesThatNeedIt(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{"KEY": "line1\nline2"}, &sent)}

	var copied string
	if _, _, err := Copy(context.Background(), c, d, "", capture(&copied)); err != nil {
		t.Fatal(err)
	}
	if copied != "KEY=\"line1\\nline2\"\n" {
		t.Fatalf("clipboard text is %q", copied)
	}
}

// An empty origin puts nothing on the clipboard: replacing whatever was there
// with an empty string would be a surprise, and the caller reports the zero.
func TestCopyLeavesTheClipboardAloneWhenEmpty(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{}, &sent)}

	called := false
	count, _, err := Copy(context.Background(), c, d, "", func(string) error { called = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("count is %d, want 0", count)
	}
	if called {
		t.Fatal("the clipboard was written with no secrets to put on it")
	}
}

// Without envi.toml the credential names the environment, the same fallback
// envi run uses in a container, so copy works there too rather than insisting
// on envi init.
func TestCopyWithoutContextReadsByCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/values" {
			t.Errorf("read %s, want /values", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project": "acme", "environment": "production", "values": map[string]string{"A": "1"},
		})
	}))
	t.Cleanup(srv.Close)

	var copied string
	count, label, err := Copy(context.Background(), Client{BaseURL: srv.URL}, t.TempDir(), "", capture(&copied))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || copied != "A=1\n" {
		t.Fatalf("copied %d secrets, text %q", count, copied)
	}
	if label != "acme · production" {
		t.Fatalf("label is %q", label)
	}
}

// A named key copies that value alone — no KEY= in front, because a single
// secret is pasted into a field that wants only the value.
func TestCopyOneCopiesTheValueAlone(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{"DATABASE_URL": "postgres://u:p@host/db", "PORT": "8080"}, &sent)}

	var copied string
	label, err := CopyOne(context.Background(), c, d, "", "DATABASE_URL", capture(&copied))
	if err != nil {
		t.Fatal(err)
	}
	if copied != "postgres://u:p@host/db" {
		t.Fatalf("clipboard holds %q", copied)
	}
	if !strings.Contains(label, "demo") {
		t.Fatalf("label is %q", label)
	}
}

// A mistyped key is the likeliest way to land here, so the error lists what is
// actually there rather than only saying no.
func TestCopyOneUnknownKeyListsTheRealOnes(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{"DATABASE_URL": "x", "PORT": "8080"}, &sent)}

	called := false
	_, err := CopyOne(context.Background(), c, d, "", "DATABSE_URL", func(string) error { called = true; return nil })
	if err == nil {
		t.Fatal("copied a key that does not exist")
	}
	for _, want := range []string{"DATABSE_URL", "DATABASE_URL", "PORT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %s", want, err)
		}
	}
	if called {
		t.Fatal("the clipboard was written for a missing key")
	}
}

// An origin with no secrets at all gets a clearer message than a key list.
func TestCopyOneWhenThereAreNoSecrets(t *testing.T) {
	d := ctxDir(t)
	var sent map[string]any
	c := Client{BaseURL: snapshotServerValues(t, 4, map[string]string{}, &sent)}

	_, err := CopyOne(context.Background(), c, d, "", "ANY", capture(new(string)))
	if err == nil || !strings.Contains(err.Error(), "no secrets") {
		t.Fatalf("got %v", err)
	}
}
