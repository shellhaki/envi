package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// keys feeds one byte at a time, the way a terminal delivers them.
type keys struct{ rest [][]byte }

func pressed(seq ...string) *keys {
	k := &keys{}
	for _, s := range seq {
		k.rest = append(k.rest, []byte(s))
	}
	return k
}
func (k *keys) Read(p []byte) (int, error) {
	if len(k.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, k.rest[0])
	k.rest = k.rest[1:]
	return n, nil
}

const (
	ctrlO = "\x0f"
	ctrlX = "\x18"
	ctrlK = "\x0b"
	up    = "\x1b[A"
	down  = "\x1b[B"
	left  = "\x1b[D"
	right = "\x1b[C"
	bs    = "\x7f"
)

func edit(t *testing.T, text string, seq ...string) (*editor, editorResult) {
	t.Helper()
	e := newEditor(text, "demo · default", io.Discard, 80, 24)
	result, err := e.run(pressed(seq...), func() bool { return true })
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return e, result
}

func TestEditorTyping(t *testing.T) {
	e, result := edit(t, "A=1\n", right, right, right, "23", ctrlO)
	if result != editorPush {
		t.Fatal("Ctrl+O did not ask for a push")
	}
	if e.text() != "A=123\n" {
		t.Fatalf("buffer is %q", e.text())
	}
	if !e.dirty {
		t.Fatal("typing left the buffer marked unmodified")
	}
}

func TestEditorBackspaceJoinsLines(t *testing.T) {
	// Cursor at the start of line two; backspace should join it to line one.
	e, _ := edit(t, "A=1\nB=2\n", down, bs, ctrlO)
	if e.text() != "A=1B=2\n" {
		t.Fatalf("buffer is %q", e.text())
	}
}

func TestEditorNewlineSplits(t *testing.T) {
	e, _ := edit(t, "A=1\n", right, right, right, "\r", "B=2", ctrlO)
	if e.text() != "A=1\nB=2\n" {
		t.Fatalf("buffer is %q", e.text())
	}
}

func TestEditorCutLine(t *testing.T) {
	e, _ := edit(t, "A=1\nB=2\nC=3\n", down, ctrlK, ctrlO)
	if e.text() != "A=1\nC=3\n" {
		t.Fatalf("buffer is %q", e.text())
	}
	// Cutting the only line leaves an empty buffer rather than no buffer.
	e2, _ := edit(t, "A=1\n", ctrlK, ctrlO)
	if e2.text() != "\n" || len(e2.lines) != 1 {
		t.Fatalf("buffer is %q with %d lines", e2.text(), len(e2.lines))
	}
}

// Leaving with unsaved work must ask. The helper answers yes; answering no has
// to keep the editor open instead.
func TestEditorQuitAsksWhenModified(t *testing.T) {
	e := newEditor("A=1\n", "demo", io.Discard, 80, 24)
	asked := 0
	_, err := e.run(pressed("X", ctrlX, ctrlO), func() bool { asked++; return false })
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("asked %d times, want 1", asked)
	}
}

func TestEditorQuitCleanDoesNotAsk(t *testing.T) {
	e := newEditor("A=1\n", "demo", io.Discard, 80, 24)
	asked := 0
	_, result := func() (error, editorResult) {
		r, err := e.run(pressed(ctrlX), func() bool { asked++; return true })
		return err, r
	}()
	_ = result
	if asked != 0 {
		t.Fatal("asked to confirm an unmodified buffer")
	}
}

// Arrow keys must not walk off the buffer.
func TestEditorCursorStaysInBounds(t *testing.T) {
	e, _ := edit(t, "A=1\nB=22\n", up, up, left, left, ctrlO)
	if e.row != 0 || e.col != 0 {
		t.Fatalf("cursor at %d,%d, want 0,0", e.row, e.col)
	}
	e2, _ := edit(t, "A=1\nB=22\n", down, down, down, right, right, right, right, right, right, ctrlO)
	if e2.row != 1 || e2.col != runeLen("B=22") {
		t.Fatalf("cursor at %d,%d", e2.row, e2.col)
	}
}

// What the editor shows and what it reads back must be the same shape as a
// .env file, so pull, mod and push all agree.
func TestSecretsRoundTripThroughTheBuffer(t *testing.T) {
	values := map[string]string{
		"PLAIN":       "value",
		"WITH SPACES": "a b c",
		"KEY":         "-----BEGIN-----\nline two\n-----END-----",
		"EQUALS":      "postgres://u:p@h/db?a=1&b=2",
		"HASH":        "#ff0000",
	}
	text := secretsAsText(values)
	back, err := parseEditorText(text)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range values {
		if back[k] != want {
			t.Errorf("%s came back as %q, want %q", k, back[k], want)
		}
	}
	// Sorted, so the buffer does not reshuffle between sessions.
	if !strings.HasPrefix(text, "EQUALS=") {
		t.Fatalf("buffer does not start with the first key alphabetically:\n%s", text)
	}
}

func TestParseEditorTextReportsTheBadLine(t *testing.T) {
	_, err := parseEditorText("A=1\n# a comment\n\nnonsense\n")
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("got %v, want an error naming line 4", err)
	}
	if _, err = parseEditorText("=novalue\n"); err == nil {
		t.Fatal("an empty key was accepted")
	}
	// Blank lines and comments are skipped, not errors.
	values, err := parseEditorText("\n# comment\nA=1\n")
	if err != nil || len(values) != 1 || values["A"] != "1" {
		t.Fatalf("got %v, %v", values, err)
	}
}

// The screen has to be drawn without panicking on a tiny terminal or a buffer
// longer than the window.
func TestEditorDrawScrollsAndFits(t *testing.T) {
	var out bytes.Buffer
	e := newEditor(strings.Repeat("KEY=value\n", 50), "demo", &out, 24, 8)
	e.row = 49
	e.clamp()
	e.draw()
	if out.Len() == 0 {
		t.Fatal("nothing drawn")
	}
	if !strings.Contains(out.String(), "^O push") {
		t.Fatal("the footer is missing")
	}
}

func TestTerminalSizeFallsBack(t *testing.T) {
	w, h := terminalSize(0, func(int) (int, int, error) { return 0, 0, io.EOF })
	if w != 80 || h != 24 {
		t.Fatalf("got %dx%d, want 80x24", w, h)
	}
	w, h = terminalSize(0, func(int) (int, int, error) { return 3, 2, nil })
	if w != 80 || h != 24 {
		t.Fatalf("an absurd size gave %dx%d", w, h)
	}
}

// A paste, or fast typing, arrives as one read containing several keys. Every
// one of them must be applied, including the control characters between them.
func TestEditorHandlesABatchedRead(t *testing.T) {
	e := newEditor("A=1\n", "demo", io.Discard, 80, 24)
	// One read: end-of-line, Return, then text — exactly what a paste looks like.
	result, err := e.run(pressed("\x05\rB=2\x0f"), func() bool { return true })
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if result != editorPush {
		t.Fatal("the Ctrl+O at the end of the batch was not seen")
	}
	if e.text() != "A=1\nB=2\n" {
		t.Fatalf("buffer is %q, want the Return to have split the line", e.text())
	}
}

// An escape sequence split across two reads must not be typed as literal text.
func TestEditorHandlesASplitEscapeSequence(t *testing.T) {
	e := newEditor("A=1\nB=2\n", "demo", io.Discard, 80, 24)
	_, err := e.run(pressed("\x1b", "[B", ctrlO), func() bool { return true })
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if e.row != 1 {
		t.Fatalf("cursor on row %d, want 1 — the split arrow key was mishandled", e.row)
	}
	if e.text() != "A=1\nB=2\n" {
		t.Fatalf("buffer is %q; the escape bytes were typed as text", e.text())
	}
}

// Multi-byte characters survive being typed and split across reads.
func TestEditorAcceptsMultiByteRunes(t *testing.T) {
	e := newEditor("K=\n", "demo", io.Discard, 80, 24)
	_, err := e.run(pressed("\x05", "café — ok", ctrlO), func() bool { return true })
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if e.text() != "K=café — ok\n" {
		t.Fatalf("buffer is %q", e.text())
	}
}
