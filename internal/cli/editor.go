package cli

// A small full-screen editor for an environment's secrets, so they can be
// changed without ever writing a .env to disk.
//
// Keys follow nano, which is what most people reach for in a terminal:
//
//	Ctrl+O   push the buffer back to Envi
//	Ctrl+X   leave (asks first if the buffer changed)
//	Ctrl+K   delete the current line
//
// The buffer is the same KEY=VALUE text .env uses, so what you edit here and
// what envi pull writes are the same thing.

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// editor holds the buffer and where the cursor is in it.
type editor struct {
	lines  []string
	row    int // index into lines
	col    int // rune offset within lines[row]
	dirty  bool
	status string
	title  string

	width, height int
	out           io.Writer
}

func newEditor(text, title string, out io.Writer, width, height int) *editor {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &editor{lines: lines, title: title, out: out, width: width, height: height}
}

// text returns the buffer as it would be written to a file.
func (e *editor) text() string {
	return strings.Join(e.lines, "\n") + "\n"
}

// runeLen counts runes, since the cursor moves by character not byte.
func runeLen(s string) int { return len([]rune(s)) }

func (e *editor) line() string { return e.lines[e.row] }

func (e *editor) setLine(s string) {
	e.lines[e.row] = s
	e.dirty = true
}

func (e *editor) insert(r rune) {
	line := []rune(e.line())
	if e.col > len(line) {
		e.col = len(line)
	}
	line = append(line[:e.col], append([]rune{r}, line[e.col:]...)...)
	e.setLine(string(line))
	e.col++
}

func (e *editor) backspace() {
	if e.col > 0 {
		line := []rune(e.line())
		e.setLine(string(append(line[:e.col-1], line[e.col:]...)))
		e.col--
		return
	}
	if e.row == 0 {
		return
	}
	// Join with the line above, landing the cursor where they meet.
	above := e.lines[e.row-1]
	e.col = runeLen(above)
	e.lines[e.row-1] = above + e.line()
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
	e.dirty = true
}

func (e *editor) newline() {
	line := []rune(e.line())
	if e.col > len(line) {
		e.col = len(line)
	}
	rest := string(line[e.col:])
	e.setLine(string(line[:e.col]))
	e.lines = append(e.lines, "")
	copy(e.lines[e.row+2:], e.lines[e.row+1:])
	e.lines[e.row+1] = rest
	e.row++
	e.col = 0
}

func (e *editor) deleteLine() {
	if len(e.lines) == 1 {
		e.setLine("")
		e.col = 0
		return
	}
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
	e.col = 0
	e.dirty = true
}

// clamp keeps the cursor inside the buffer after any movement.
func (e *editor) clamp() {
	if e.row < 0 {
		e.row = 0
	}
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
	if n := runeLen(e.line()); e.col > n {
		e.col = n
	}
	if e.col < 0 {
		e.col = 0
	}
}

// visibleRows is the buffer area, leaving room for the title and two footer
// lines.
func (e *editor) visibleRows() int {
	n := e.height - 4
	if n < 1 {
		n = 1
	}
	return n
}

// draw repaints the whole screen. Small buffers make this cheap enough that
// tracking dirty regions would only add ways to be wrong.
func (e *editor) draw() {
	rows := e.visibleRows()
	top := 0
	if e.row >= rows {
		top = e.row - rows + 1
	}

	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J") // home, clear
	fmt.Fprintf(&b, "\x1b[7m %-*s\x1b[0m\r\n", e.width-1, "envi mod — "+e.title)

	for i := 0; i < rows; i++ {
		n := top + i
		if n < len(e.lines) {
			line := e.lines[n]
			if runeLen(line) > e.width {
				line = string([]rune(line)[:e.width])
			}
			b.WriteString(line)
		}
		b.WriteString("\r\n")
	}

	status := e.status
	if status == "" {
		status = fmt.Sprintf("%d line%s%s", len(e.lines), plural(len(e.lines)), map[bool]string{true: " · modified", false: ""}[e.dirty])
	}
	fmt.Fprintf(&b, "\x1b[2m%-*s\x1b[0m\r\n", e.width-1, truncate(status, e.width-1))
	fmt.Fprintf(&b, "\x1b[7m %s \x1b[0m \x1b[7m %s \x1b[0m \x1b[7m %s \x1b[0m", "^O push", "^X exit", "^K cut line")

	// Place the cursor: rows are 1-based and the title takes the first.
	fmt.Fprintf(&b, "\x1b[%d;%dH", e.row-top+2, e.col+1)
	io.WriteString(e.out, b.String())
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}
	return string(r[:n])
}

// editorResult says what the editor was asked to do when it returned.
type editorResult int

const (
	editorQuit editorResult = iota
	editorPush
)

// run reads keys until the user pushes or quits. confirm is called when
// quitting with unsaved changes; returning false keeps editing.
//
// A read can carry several keys at once — a paste, or fast typing — so the
// buffer is consumed byte by byte rather than matched as a whole. Switching on
// the length of the read would drop a Return that arrived alongside the text
// after it.
func (e *editor) run(in io.Reader, confirm func() bool) (editorResult, error) {
	buf := make([]byte, 1024)
	pending := []byte{}
	for {
		e.draw()
		n, err := in.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			result, quit, consumed := e.consume(pending, confirm)
			pending = consumed
			if quit {
				return result, nil
			}
		}
		if err != nil {
			return editorQuit, err
		}
	}
}

// consume applies every complete key in the buffer, returning whatever is left
// over: a partial escape sequence split across two reads waits for the rest.
func (e *editor) consume(in []byte, confirm func() bool) (result editorResult, quit bool, rest []byte) {
	e.status = ""
	for i := 0; i < len(in); {
		b := in[i]

		// An escape sequence: ESC [ <letter>. Wait if it is not all here yet.
		if b == 0x1b {
			if i+2 >= len(in) {
				return editorQuit, false, in[i:]
			}
			if in[i+1] == '[' {
				e.move(in[i+2])
				i += 3
				continue
			}
			i++ // a bare ESC, ignored
			continue
		}

		switch b {
		case 15: // Ctrl+O
			return editorPush, true, nil
		case 24: // Ctrl+X
			if e.dirty && !confirm() {
				e.status = "Still editing."
				i++
				continue
			}
			return editorQuit, true, nil
		case 11: // Ctrl+K
			e.deleteLine()
		case 1: // Ctrl+A
			e.col = 0
		case 5: // Ctrl+E
			e.col = runeLen(e.line())
		case 127, 8:
			e.backspace()
		case '\r', '\n':
			e.newline()
		default:
			if b >= 32 {
				// Decode one rune, so multi-byte characters survive.
				r, size := decodeRune(in[i:])
				if size == 0 {
					return editorQuit, false, in[i:] // incomplete, wait
				}
				e.insert(r)
				i += size
				continue
			}
		}
		i++
	}
	return editorQuit, false, nil
}

// move applies an arrow or home/end key.
func (e *editor) move(key byte) {
	switch key {
	case 'A':
		e.row--
	case 'B':
		e.row++
	case 'C':
		e.col++
	case 'D':
		e.col--
	case 'H':
		e.col = 0
	case 'F':
		e.col = runeLen(e.line())
	}
	e.clamp()
}

// decodeRune reads one UTF-8 rune, returning size 0 when the bytes are a
// prefix of a character whose remainder has not arrived yet.
func decodeRune(b []byte) (rune, int) {
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 && !utf8.FullRune(b) {
		return 0, 0
	}
	if r == utf8.RuneError && size <= 1 {
		return 0, 1 // invalid byte: skip it
	}
	return r, size
}

// secretsAsText renders secrets in .env order: sorted, quoted the same way
// envi pull writes them, so the buffer is byte-identical to the file it replaces.
func secretsAsText(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, quoteValue(values[k]))
	}
	return b.String()
}

// parseEditorText reads the buffer back, reporting the first bad line by
// number rather than failing with a message nobody can act on.
func parseEditorText(text string) (map[string]string, error) {
	values := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		at := strings.IndexByte(trimmed, '=')
		if at <= 0 {
			return nil, fmt.Errorf("line %d is not KEY=VALUE: %s", i+1, truncate(trimmed, 40))
		}
		key := strings.TrimSpace(trimmed[:at])
		if key == "" {
			return nil, fmt.Errorf("line %d has an empty key", i+1)
		}
		values[key] = unquoteValue(strings.TrimSpace(trimmed[at+1:]))
	}
	return values, nil
}

var errNotATerminal = errors.New("envi mod needs an interactive terminal; use envi pull and envi push instead")

// terminalSize falls back to a sane default when the size cannot be read, so a
// slightly odd terminal still gets a usable screen.
func terminalSize(fd int, size func(int) (int, int, error)) (int, int) {
	w, h, err := size(fd)
	if err != nil || w < 20 || h < 6 {
		return 80, 24
	}
	return w, h
}
