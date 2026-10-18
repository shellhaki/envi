package cli

// A one-of-many list for the terminal: arrow keys to move, type to narrow, Return to choose.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

// How many rows of the list are on screen at once.
const pickerWindow = 10

var errPickerCancelled = errors.New("cancelled")

// picker holds the list's state: items, the typed filter and the cursor.
type picker struct {
	items   []string
	filter  string
	cursor  int   // index into matches, not into items
	matches []int // indexes into items, in display order
}

func newPicker(items []string) *picker {
	p := &picker{items: items}
	p.refilter()
	return p
}

// refilter recomputes the visible set.
func (p *picker) refilter() {
	p.matches = p.matches[:0]
	needle := strings.ToLower(p.filter)
	for i, item := range p.items {
		if subsequence(strings.ToLower(item), needle) {
			p.matches = append(p.matches, i)
		}
	}
	if p.cursor >= len(p.matches) {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// subsequence reports whether needle's characters appear in item in order, e.g. "db" in DATABASE_URL.
func subsequence(item, needle string) bool {
	if needle == "" {
		return true
	}
	at := 0
	for i := 0; i < len(item) && at < len(needle); i++ {
		if item[i] == needle[at] {
			at++
		}
	}
	return at == len(needle)
}

func (p *picker) move(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor += delta
	// The cursor wraps around at both ends.
	if p.cursor < 0 {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
}

// selected reports the chosen item, or false when the filter matches nothing.
func (p *picker) selected() (string, bool) {
	if len(p.matches) == 0 {
		return "", false
	}
	return p.items[p.matches[p.cursor]], true
}

// window returns the part of the list to draw so the cursor stays visible.
func (p *picker) window() (rows []int, offset int) {
	if len(p.matches) <= pickerWindow {
		return p.matches, 0
	}
	offset = p.cursor - pickerWindow/2
	if offset < 0 {
		offset = 0
	}
	if offset > len(p.matches)-pickerWindow {
		offset = len(p.matches) - pickerWindow
	}
	return p.matches[offset : offset+pickerWindow], offset
}

// pickerAction is what a keystroke asked for.
type pickerAction int

const (
	pickerContinue pickerAction = iota
	pickerChoose
	pickerCancel
)

// key applies one keystroke.
func (p *picker) key(b byte, escaped bool) pickerAction {
	if escaped {
		switch b {
		case 'A': // up
			p.move(-1)
		case 'B': // down
			p.move(1)
		}
		return pickerContinue
	}

	switch b {
	case '\r', '\n':
		if _, ok := p.selected(); ok {
			return pickerChoose
		}
	case 3, 27: // Ctrl+C, Escape
		return pickerCancel
	case 127, 8: // Backspace
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.refilter()
		}
	case 21: // Ctrl+U clears the filter outright
		p.filter = ""
		p.refilter()
	case 14: // Ctrl+N
		p.move(1)
	case 16: // Ctrl+P
		p.move(-1)
	default:
		if b >= 32 && b < 127 {
			p.filter += string(b)
			p.refilter()
		}
	}
	return pickerContinue
}

// runPicker draws the list and reads keys until an item is chosen or the list is cancelled.
func runPicker(in *os.File, out io.Writer, title string, items []string) (string, error) {
	if !term.IsTerminal(int(in.Fd())) {
		return "", errors.New("envi copy --single needs an interactive terminal; name the secret instead, as envi copy KEY")
	}

	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	p := newPicker(sorted)

	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("could not take over the terminal: %w", err)
	}
	drawn := 0
	defer func() {
		erase(out, drawn)
		_ = term.Restore(fd, state)
		fmt.Fprint(out, "\x1b[?25h") // cursor back
	}()
	fmt.Fprint(out, "\x1b[?25l") // hide the cursor while the list redraws

	buf := make([]byte, 64)
	for {
		erase(out, drawn)
		drawn = drawPicker(out, title, p)

		n, err := in.Read(buf)
		if err != nil {
			return "", err
		}
		for i := 0; i < n; i++ {
			escaped := false
			b := buf[i]
			// An arrow key is ESC [ <letter>.
			if b == 0x1b && i+2 < n && buf[i+1] == '[' {
				escaped, b = true, buf[i+2]
				i += 2
			}
			switch p.key(b, escaped) {
			case pickerChoose:
				chosen, _ := p.selected()
				return chosen, nil
			case pickerCancel:
				return "", errPickerCancelled
			}
		}
	}
}

// erase removes the n lines the last draw left behind.
func erase(out io.Writer, n int) {
	for i := 0; i < n; i++ {
		fmt.Fprint(out, "\x1b[1A\x1b[2K")
	}
	if n > 0 {
		fmt.Fprint(out, "\r")
	}
}

// drawPicker writes the list and returns how many lines it used, so the next redraw can erase them.
func drawPicker(out io.Writer, title string, p *picker) int {
	lines := 0
	write := func(format string, a ...any) {
		fmt.Fprintf(out, format+"\r\n", a...)
		lines++
	}

	heading := title
	if p.filter != "" {
		heading += "   " + ansiBold + p.filter + ansiReset
	}
	write("%s", heading)

	rows, offset := p.window()
	if len(rows) == 0 {
		write("  %sno secret matches %q%s", ansiDim, p.filter, ansiReset)
	}
	for i, item := range rows {
		if offset+i == p.cursor {
			write("%s❯ %s%s", ansiBold, p.items[item], ansiReset)
			continue
		}
		write("  %s", p.items[item])
	}

	more := ""
	if len(p.matches) > len(rows) {
		more = fmt.Sprintf(" · %d of %d", len(rows), len(p.matches))
	}
	write("%s↑↓ move · type to filter · enter copy · esc cancel%s%s", ansiDim, more, ansiReset)
	return lines
}
