package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// UI is the CLI's presentation layer.
type UI struct {
	Out   io.Writer
	Style bool
}

func NewUI(w io.Writer) UI { return UI{Out: w, Style: styled(w)} }

// styled reports whether w is an interactive terminal that wants escape codes.
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

// Success, Warn, Fail and Step are the whole vocabulary.
func (u UI) marker(color, glyph, plain string) string {
	if !u.Style {
		return plain
	}
	return u.paint(color, glyph) + " "
}
func (u UI) Success(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s%s\n", u.marker(ansiGreen, "✓", ""), fmt.Sprintf(format, a...))
}
func (u UI) Warn(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s%s\n", u.marker(ansiYellow, "!", "warning: "), fmt.Sprintf(format, a...))
}
func (u UI) Fail(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s%s\n", u.marker(ansiRed, "✗", "error: "), fmt.Sprintf(format, a...))
}
func (u UI) Step(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s%s\n", u.marker(ansiDim, "›", ""), fmt.Sprintf(format, a...))
}
func (u UI) Print(format string, a ...any) { fmt.Fprintf(u.Out, format+"\n", a...) }

// Spinner shows that a slow network call is still working.
type Spinner struct {
	ui    UI
	label string
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (u UI) Spinner(label string) *Spinner {
	s := &Spinner{ui: u, label: label, stop: make(chan struct{}), done: make(chan struct{})}
	if !u.Style {
		close(s.done)
		return s
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.done)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for i := 0; ; i++ {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			fmt.Fprintf(s.ui.Out, "\r%s %s", s.ui.paint(ansiYellow, spinnerFrames[i%len(spinnerFrames)]), s.ui.Dim(s.label))
		}
	}
}

// Stop ends the animation and clears its line.
func (s *Spinner) Stop() {
	s.once.Do(func() {
		close(s.stop)
		<-s.done
		if s.ui.Style {
			fmt.Fprintf(s.ui.Out, "\r%s\r", strings.Repeat(" ", len(s.label)+4))
		}
	})
}

// Writer wraps w so the first byte a command prints stops the spinner first.
func (s *Spinner) Writer(w io.Writer) io.Writer { return &spinnerWriter{s: s, w: w} }

type spinnerWriter struct {
	s *Spinner
	w io.Writer
}

func (sw *spinnerWriter) Write(b []byte) (int, error) {
	sw.s.Stop()
	return sw.w.Write(b)
}

// Progress draws a download bar that redraws in place on a terminal.
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

// Wrap returns a reader that advances the bar as it is read.
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
	// Redraw at most ~20 times a second.
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

// Done closes out the bar.
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

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
