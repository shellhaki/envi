package cli

import (
	"strings"
	"testing"
)

func secretNames() []string {
	return []string{"API_KEY", "DATABASE_URL", "DB_PASSWORD", "PORT", "STRIPE_KEY"}
}

// Typing narrows the list, case-insensitively and on any part of the name —
// nobody remembers which end of a key the memorable part is on.
func TestPickerFilters(t *testing.T) {
	p := newPicker(secretNames())
	for _, b := range []byte("db") {
		p.key(b, false)
	}
	var shown []string
	for _, i := range p.matches {
		shown = append(shown, p.items[i])
	}
	if strings.Join(shown, ",") != "DATABASE_URL,DB_PASSWORD" {
		t.Fatalf("filter %q shows %v", p.filter, shown)
	}
}

// Backspace takes the filter back; Ctrl+U drops it entirely.
func TestPickerClearsTheFilter(t *testing.T) {
	p := newPicker(secretNames())
	for _, b := range []byte("port") {
		p.key(b, false)
	}
	if len(p.matches) != 1 {
		t.Fatalf("filter %q matched %d", p.filter, len(p.matches))
	}
	p.key(127, false)
	if p.filter != "por" {
		t.Fatalf("after backspace the filter is %q", p.filter)
	}
	p.key(21, false)
	if p.filter != "" || len(p.matches) != len(secretNames()) {
		t.Fatalf("Ctrl+U left filter %q with %d matches", p.filter, len(p.matches))
	}
}

// The cursor wraps rather than stopping: a list you can run off the end of
// makes you check whether you ran off the end of it.
func TestPickerWraps(t *testing.T) {
	p := newPicker(secretNames())
	p.key('A', true) // up from the first item
	if got, _ := p.selected(); got != "STRIPE_KEY" {
		t.Fatalf("up from the top selected %q", got)
	}
	p.key('B', true) // back down
	if got, _ := p.selected(); got != "API_KEY" {
		t.Fatalf("down from the bottom selected %q", got)
	}
}

// Narrowing past the cursor must move it back onto the list, or Return would
// choose an item that is no longer shown.
func TestPickerKeepsTheCursorOnScreen(t *testing.T) {
	p := newPicker(secretNames())
	p.key('B', true)
	p.key('B', true)
	p.key('B', true)
	p.key('B', true) // on STRIPE_KEY, the last item
	for _, b := range []byte("api") {
		p.key(b, false)
	}
	got, ok := p.selected()
	if !ok || got != "API_KEY" {
		t.Fatalf("after narrowing, selected %q (%v)", got, ok)
	}
}

// Return on a filter that matches nothing does nothing at all, rather than
// choosing or cancelling.
func TestPickerReturnWithNoMatch(t *testing.T) {
	p := newPicker(secretNames())
	for _, b := range []byte("zzz") {
		p.key(b, false)
	}
	if action := p.key('\r', false); action != pickerContinue {
		t.Fatalf("Return with no match returned %v", action)
	}
}

func TestPickerCancels(t *testing.T) {
	for _, b := range []byte{3, 27} {
		if action := newPicker(secretNames()).key(b, false); action != pickerCancel {
			t.Fatalf("key %d returned %v, want cancel", b, action)
		}
	}
}

func TestPickerChooses(t *testing.T) {
	p := newPicker(secretNames())
	p.key('B', true)
	if action := p.key('\r', false); action != pickerChoose {
		t.Fatalf("Return returned %v, want choose", action)
	}
	if got, _ := p.selected(); got != "DATABASE_URL" {
		t.Fatalf("chose %q", got)
	}
}

// A list longer than the window scrolls, and the cursor stays inside it.
func TestPickerWindowFollowsTheCursor(t *testing.T) {
	many := make([]string, 40)
	for i := range many {
		many[i] = string(rune('A'+i%26)) + "_KEY"
	}
	p := newPicker(many)
	for i := 0; i < 30; i++ {
		p.key('B', true)
	}
	rows, offset := p.window()
	if len(rows) != pickerWindow {
		t.Fatalf("window holds %d rows, want %d", len(rows), pickerWindow)
	}
	if p.cursor < offset || p.cursor >= offset+len(rows) {
		t.Fatalf("cursor %d is outside the window [%d,%d)", p.cursor, offset, offset+len(rows))
	}
}

// Matching is by subsequence, not substring: "dburl" has to find DATABASE_URL,
// because that is the key somebody typing it is reaching for.
func TestPickerMatchesSubsequences(t *testing.T) {
	p := newPicker(secretNames())
	for _, b := range []byte("dburl") {
		p.key(b, false)
	}
	got, ok := p.selected()
	if !ok || got != "DATABASE_URL" {
		t.Fatalf("filter %q selected %q (%v)", p.filter, got, ok)
	}
	if len(p.matches) != 1 {
		t.Fatalf("filter %q matched %d items, want 1", p.filter, len(p.matches))
	}
}
