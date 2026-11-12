package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Anything that is not a terminal — a pipe, a CI log, a command substitution —
// must get plain text. `TOKEN=$(envi token create ...)` depends on it.
func TestUIPlainWhenNotATerminal(t *testing.T) {
	var out bytes.Buffer
	ui := NewUI(&out) // a bytes.Buffer is never a char device
	if ui.Style {
		t.Fatal("styling enabled for a non-terminal writer")
	}
	ui.Success("Pushed %d secrets", 3)
	ui.Step("Checking")
	ui.Warn("careful")
	ui.Fail("broken")
	got := out.String()
	if strings.ContainsRune(got, '\033') {
		t.Fatalf("escape sequences leaked into non-terminal output: %q", got)
	}
	for _, glyph := range []string{"✓", "✗", "›", "█", "░"} {
		if strings.Contains(got, glyph) {
			t.Fatalf("glyph %q leaked into non-terminal output: %q", glyph, got)
		}
	}
	want := "Pushed 3 secrets\nChecking\nwarning: careful\nerror: broken\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUIStyledWritesColour(t *testing.T) {
	var out bytes.Buffer
	ui := UI{Out: &out, Style: true}
	ui.Success("done")
	got := out.String()
	if !strings.Contains(got, ansiGreen) || !strings.Contains(got, "✓") {
		t.Fatalf("expected a green check, got %q", got)
	}
}

// A spinner on a pipe must be completely silent, or it would corrupt whatever
// the command actually printed.
func TestSpinnerSilentWhenNotATerminal(t *testing.T) {
	var out bytes.Buffer
	ui := NewUI(&out)
	s := ui.Spinner("Working")
	time.Sleep(200 * time.Millisecond)
	s.Stop()
	if out.Len() != 0 {
		t.Fatalf("spinner wrote %q to a non-terminal", out.String())
	}
}

func TestSpinnerStopIsIdempotent(t *testing.T) {
	ui := UI{Out: &bytes.Buffer{}, Style: true}
	s := ui.Spinner("Working")
	s.Stop()
	s.Stop() // must not panic on a second close
}

// The writer handed to commands stops the spinner before their first byte, so
// streamed output never interleaves with the animation.
func TestSpinnerWriterStopsBeforeFirstWrite(t *testing.T) {
	var out bytes.Buffer
	ui := UI{Out: &out, Style: true}
	s := ui.Spinner("Working")
	w := s.Writer(&out)
	if _, err := w.Write([]byte("added KEY\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "added KEY\n") {
		t.Fatalf("command output was not last; spinner still drawing: %q", out.String())
	}
}

func TestProgressOffTerminalPrintsOneSummaryLine(t *testing.T) {
	var out bytes.Buffer
	ui := NewUI(&out)
	p := ui.Progress("downloading", 2048)
	p.Add(2048)
	p.Done()
	got := out.String()
	if strings.Count(got, "\n") != 1 || strings.Contains(got, "\r") {
		t.Fatalf("expected a single clean line, got %q", got)
	}
	if !strings.Contains(got, "2 KB") {
		t.Fatalf("expected a human size, got %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{512: "512 B", 2048: "2 KB", 5 << 20: "5.0 MB"}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
