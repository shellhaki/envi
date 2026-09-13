package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// UI is the CLI's presentation layer. Styling is decided once, at construction,
// from the writer it was given: piping to a file or a CI log yields plain text
// with no escape codes and no in-place redraws, so `envi pull > out.txt` stays
// machine-readable and the test suite keeps asserting on clean strings.
type UI struct {
	Out   io.Writer
	Style bool
}

func NewUI(w io.Writer) UI { return UI{Out: w, Style: styled(w)} }

// styled reports whether w is an interactive terminal that wants escape codes.
// NO_COLOR is honoured (https://no-color.org), as is TERM=dumb.
func styled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

func (u UI) paint(code, text string) string {
	if !u.Style {
		return text
	}
	return code + text + ansiReset
}

func (u UI) Bold(text string) string { return u.paint(ansiBold, text) }
func (u UI) Dim(text string) string  { return u.paint(ansiDim, text) }

// Success, Warn, Fail and Step are the whole vocabulary. Commands stay quiet by
// default — a marker and a sentence — so the only place that draws anything
// elaborate is a download, where there is genuinely something to watch.
func (u UI) Success(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.paint(ansiGreen, "✓"), fmt.Sprintf(format, a...))
}
func (u UI) Warn(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.paint(ansiYellow, "!"), fmt.Sprintf(format, a...))
}
func (u UI) Fail(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.paint(ansiRed, "✗"), fmt.Sprintf(format, a...))
}
func (u UI) Step(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.Dim("›"), fmt.Sprintf(format, a...))
}
func (u UI) Print(format string, a ...any) { fmt.Fprintf(u.Out, format+"\n", a...) }

// Progress draws a download bar that redraws in place on a terminal. Off a
// terminal it prints one line when the transfer finishes, so logs get a record
// of what happened without thousands of carriage returns.
type Progress struct {
	ui     UI
	label  string
	total  int64
	mu     sync.Mutex
	done   int64
	lastAt time.Time
	width  int
	drawn  bool
}

func (u UI) Progress(label string, total int64) *Progress {
	return &Progress{ui: u, label: label, total: total, width: 28}
}

// Wrap returns a reader that advances the bar as it is read, so the caller just
// copies from it and the drawing takes care of itself.
func (p *Progress) Wrap(r io.Reader) io.Reader { return &progressReader{p: p, r: r} }

type progressReader struct {
	p *Progress
	r io.Reader
}

func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if n > 0 {
		pr.p.Add(int64(n))
	}
	return n, err
}

func (p *Progress) Add(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	if !p.ui.Style {
		return
	}
	// ~20fps is smooth to the eye and cheap; redrawing per chunk would spend
	// more time writing escape codes than reading the socket.
	if time.Since(p.lastAt) < 50*time.Millisecond && p.done < p.total {
		return
	}
	p.lastAt = time.Now()
	p.draw()
}

func (p *Progress) draw() {
	filled := 0
	if p.total > 0 {
		filled = int(float64(p.width) * float64(p.done) / float64(p.total))
		if filled > p.width {
			filled = p.width
		}
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)
	fmt.Fprintf(p.ui.Out, "\r  %s %s %s", p.label, p.ui.paint(ansiGreen, bar), p.ui.Dim(p.stats()))
	p.drawn = true
}

func (p *Progress) stats() string {
	if p.total <= 0 {
		return humanBytes(p.done)
	}
	return fmt.Sprintf("%3.0f%%  %s / %s", 100*float64(p.done)/float64(p.total), humanBytes(p.done), humanBytes(p.total))
}

// Done closes out the bar. On a terminal it clears the line so the caller can
// print a normal result in its place; elsewhere it emits the single summary.
func (p *Progress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ui.Style {
		fmt.Fprintf(p.ui.Out, "  %s %s\n", p.label, humanBytes(p.done))
		return
	}
	if p.drawn {
		// Two spaces past the widest line we draw, so no stale glyphs remain.
		fmt.Fprintf(p.ui.Out, "\r%s\r", strings.Repeat(" ", p.width+len(p.label)+34))
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
