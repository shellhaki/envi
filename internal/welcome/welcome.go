// Package welcome sends the one email somebody gets for signing up.
//
// It is deliberately a note from a person rather than a product announcement.
// Somebody who has just handed over their email to a secrets manager built by
// a stranger is deciding whether to trust it, and a templated marketing blast
// is evidence against. The From name carries that too — see SenderNote.
package welcome

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"shellhaki/envi/internal/mailer"
)

/*
SenderNote records a configuration fact that is not visible in this file.

The display name on the email comes from RESEND_FROM, not from here, so the
"Haki from Envi" part of the sender is set in the environment:

	RESEND_FROM=Haki from Envi <haki@envisecrets.com>

With a bare address there, the mail still sends and still reads correctly — it
just arrives from "haki@envisecrets.com" instead of from a person.
*/
const SenderNote = "RESEND_FROM carries the display name, e.g. `Haki from Envi <haki@envisecrets.com>`"

// Service sends the welcome. WebURL and DocsURL are woven into the body so a
// self-hosted instance links to itself rather than to the hosted product.
type Service struct {
	Mailer  mailer.Sender
	WebURL  string
	DocsURL string
}

/*
Greet emails somebody who has just signed up.

It never returns an error, because there is nothing a caller could usefully do
with one: the account exists either way, and failing a sign-up because an SMTP
hop timed out would be a worse outcome than a missing email. Failures are
logged and dropped.

It runs on its own context with a deadline rather than the request's. The
request that triggered it is finished, and a context cancelled by the reply
going out would cancel this with it.
*/
func (s Service) Greet(email string) {
	if s.Mailer == nil || strings.TrimSpace(email) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		done := make(chan error, 1)
		go func() { done <- s.Mailer.Send(s.message(email)) }()

		select {
		case err := <-done:
			if err != nil {
				log.Printf("welcome email to %s failed: %v", email, err)
			}
		case <-ctx.Done():
			log.Printf("welcome email to %s timed out", email)
		}
	}()
}

func (s Service) message(to string) mailer.Message {
	web := strings.TrimRight(orDefault(s.WebURL, "https://envisecrets.com"), "/")
	docs := strings.TrimRight(orDefault(s.DocsURL, "https://docs.envisecrets.com"), "/")

	return mailer.Message{
		To: to,
		// No exclamation mark and no "welcome aboard": subject lines that read
		// as a campaign are scored as one.
		Subject: "You're home — a note from Haki at Envi",
		Text:    text(web, docs),
		HTML:    htmlBody(web, docs),
		Headers: map[string]string{
			// Gmail and Yahoo weigh this when deciding whether bulk mail was
			// wanted. A sender without it is judged harder, and this is a
			// one-off note, so honouring it costs nothing.
			"List-Unsubscribe": fmt.Sprintf("<mailto:unsubscribe@%s?subject=unsubscribe>", hostOf(web)),
		},
	}
}

// hostOf strips the scheme, for building a mailto out of a web URL.
func hostOf(url string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(host, "/:"); i > 0 {
		host = host[:i]
	}
	return host
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// text is the plain-text part, and it is a real letter rather than a stripped
// copy of the HTML. Plenty of people read mail as text, and a wall of orphaned
// link markup is how a sender ends up in a spam folder.
func text(web, docs string) string {
	return fmt.Sprintf(`Hey,

You're home.

I'm Haki. I built Envi, and I wanted to say thanks for signing up.

Envi keeps your environment secrets encrypted, scoped to the people who
should see them, and out of the places they usually end up: a chat
thread, someone's downloads folder, a commit nobody noticed.

The idea is that your secrets never sit on anyone's laptop as a file.
You pull them into the process that needs them, for exactly as long as
it runs, and nothing is left behind afterwards.

Your dashboard is ready whenever you are:
%s

The docs walk through the first few minutes:
%s

If something is broken or confusing, just reply. It comes to me.

I hope you stay.

Haki
Envi
`, web, docs)
}

/*
htmlBody is the same letter, in the product's own palette.

The colours are the tokens from ui/app/globals.css, written as literal hex
because an email client has no stylesheet to inherit from and no custom
property support worth relying on. Everything is inline for the same reason —
Gmail strips <style> blocks.
*/
func htmlBody(web, docs string) string {
	const (
		bg      = "#fafafa"
		surface = "#ffffff"
		line    = "#e8e8ea"
		ink     = "#09090b"
		body    = "#3f3f46"
		muted   = "#71717a"
		faint   = "#a1a1aa"
		accent  = "#f5e73f"
	)
	font := "-apple-system,BlinkMacSystemFont,'Segoe UI',Inter,Roboto,Helvetica,Arial,sans-serif"

	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:%s;font-family:%s;color:%s;-webkit-font-smoothing:antialiased;">
<div style="max-width:560px;margin:0 auto;background:%s;border:1px solid %s;border-radius:16px;overflow:hidden;">

  <!-- The accent is a fill with near-black on it, never thin yellow type:
       the same rule the product's own interface follows. -->
  <div style="height:4px;background:%s;"></div>

  <div style="padding:36px 36px 40px;">
    <p style="margin:0 0 28px;font-size:11px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:%s;">Envi</p>

    <h1 style="margin:0 0 20px;font-size:26px;line-height:1.25;font-weight:600;letter-spacing:-.02em;color:%s;">You&rsquo;re home.</h1>

    <p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:%s;">
      I&rsquo;m Haki. I built Envi, and I wanted to say thanks for signing up.
      This is a real address &mdash; replying to it reaches me.
    </p>

    <p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:%s;">
      Envi keeps your environment secrets encrypted, scoped to the people who should see
      them, and out of the places they usually end up: a chat thread, someone&rsquo;s
      downloads folder, a commit nobody noticed.
    </p>

    <p style="margin:0 0 28px;font-size:15px;line-height:1.65;color:%s;">
      The idea is that your secrets never sit on anyone&rsquo;s laptop as a file. You pull
      them into the process that needs them, for exactly as long as it runs, and nothing
      is left behind afterwards.
    </p>

    <a href="%s" style="display:inline-block;padding:12px 22px;background:%s;color:%s;text-decoration:none;border-radius:9px;font-size:14px;font-weight:600;letter-spacing:-.01em;">Open your dashboard</a>

    <p style="margin:28px 0 0;font-size:15px;line-height:1.65;color:%s;">
      The <a href="%s" style="color:%s;">documentation</a> walks through the first few minutes.
    </p>

    <p style="margin:24px 0 0;font-size:15px;line-height:1.65;color:%s;">
      I hope you stay.<br>
      <span style="color:%s;">Haki</span>
    </p>
  </div>

  <div style="padding:18px 36px;border-top:1px solid %s;background:%s;">
    <p style="margin:0;font-size:12px;line-height:1.6;color:%s;">
      You received this once, because you created an Envi account. No list, no newsletter.
    </p>
  </div>
</div></body></html>`,
		bg, font, ink,
		surface, line,
		accent,
		faint,
		ink,
		body,
		body,
		body,
		html.EscapeString(web), ink, surface,
		body, html.EscapeString(docs), muted,
		body, muted,
		line, bg,
		faint,
	)
}
