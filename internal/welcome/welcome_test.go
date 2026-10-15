package welcome

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"shellhaki/envi/internal/mailer"
)

type recorder struct {
	mu   sync.Mutex
	sent []mailer.Message
	err  error
}

func (r *recorder) Send(m mailer.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, m)
	return r.err
}

func (r *recorder) messages() []mailer.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mailer.Message(nil), r.sent...)
}

// waitFor polls, because Greet sends on its own goroutine.
func waitFor(t *testing.T, r *recorder, n int) []mailer.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.messages(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited for %d message(s), got %d", n, len(r.messages()))
	return nil
}

func TestGreetSendsBothParts(t *testing.T) {
	r := &recorder{}
	Service{Mailer: r, WebURL: "https://envisecrets.com"}.Greet("new@example.com")

	m := waitFor(t, r, 1)[0]
	if m.To != "new@example.com" {
		t.Errorf("to = %q", m.To)
	}
	if !strings.Contains(m.Subject, "You're home") {
		t.Errorf("subject = %q", m.Subject)
	}
	// Both parts, always: a text-only or HTML-only body is itself a signal
	// spam filters weigh against a sender.
	if m.Text == "" || m.HTML == "" {
		t.Fatalf("missing a part: text %d bytes, html %d bytes", len(m.Text), len(m.HTML))
	}
	// The plain-text part is a real letter, not stripped markup.
	if strings.Contains(m.Text, "<") || strings.Contains(m.Text, "style=") {
		t.Error("the text part contains markup")
	}
	for _, want := range []string{"Haki", "You're home", "I hope you stay"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("text part is missing %q", want)
		}
		if !strings.Contains(m.HTML, want) && !strings.Contains(m.HTML, strings.ReplaceAll(want, "'", "&rsquo;")) {
			t.Errorf("html part is missing %q", want)
		}
	}
}

// A self-hosted instance must link to itself, not to the hosted product.
func TestGreetUsesTheInstanceURLs(t *testing.T) {
	r := &recorder{}
	Service{Mailer: r, WebURL: "https://envi.internal.acme", DocsURL: "https://envi.internal.acme/docs"}.
		Greet("new@example.com")

	m := waitFor(t, r, 1)[0]
	if !strings.Contains(m.HTML, "https://envi.internal.acme") {
		t.Error("html does not link to the configured instance")
	}
	if !strings.Contains(m.Text, "envi.internal.acme") {
		t.Error("text does not mention the configured instance")
	}
}

// Nothing configured still produces a sendable letter rather than a body with
// empty links in it.
func TestGreetFallsBackToTheHostedURLs(t *testing.T) {
	r := &recorder{}
	Service{Mailer: r}.Greet("new@example.com")

	m := waitFor(t, r, 1)[0]
	if !strings.Contains(m.HTML, "https://envisecrets.com") {
		t.Error("no fallback web URL")
	}
	if !strings.Contains(m.Text, "docs.envisecrets.com") {
		t.Error("no fallback docs URL")
	}
	// A shell pipeline in an email body is a malware signature to every spam
	// filter worth the name. It belongs in the docs, not in the welcome.
	for _, banned := range []string{"| sh", "curl -", "bash"} {
		if strings.Contains(m.Text, banned) || strings.Contains(m.HTML, banned) {
			t.Errorf("the body contains %q, which is scored as malware", banned)
		}
	}
	if m.Headers["List-Unsubscribe"] == "" {
		t.Error("no List-Unsubscribe header")
	}
	if strings.Contains(m.HTML, `href=""`) {
		t.Error("an empty link made it into the body")
	}
}

// A failing mailer must not panic or block. The account already exists; the
// email is the part allowed to fail.
func TestGreetSurvivesAFailingMailer(t *testing.T) {
	r := &recorder{err: errors.New("resend is down")}
	done := make(chan struct{})
	go func() {
		Service{Mailer: r}.Greet("new@example.com")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Greet blocked on a failing mailer")
	}
	waitFor(t, r, 1)
}

// No mailer and no address are both no-ops rather than panics: a self-hosted
// instance with no Resend key still has to be able to sign people up.
func TestGreetWithoutAMailerOrAddress(t *testing.T) {
	Service{}.Greet("new@example.com")

	r := &recorder{}
	Service{Mailer: r}.Greet("   ")
	time.Sleep(50 * time.Millisecond)
	if got := r.messages(); len(got) != 0 {
		t.Errorf("sent %d message(s) for a blank address", len(got))
	}
}
