// Package announce delivers one message to everybody who wants to hear from
// the instance.
//
// It sits between internal/admin, which knows who the recipients are and keeps
// the record, and internal/mailer, which does the sending. Neither of those
// should know about the other: the database package should not hold an SMTP
// connection, and the mailer should not run queries.
package announce

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/mailer"
)

// Service sends announcements.
type Service struct {
	Admin  admin.Service
	Mailer mailer.Sender
	WebURL string
	// Pause between messages. Sending a thousand emails as fast as the API
	// will take them is how a sending domain gets rate limited, or worse,
	// reclassified. A small gap costs nothing at this size.
	Pause time.Duration
}

/*
Send records the announcement, then delivers it.

The record is written first and deliberately: an announcement that fails
halfway has still gone to the people it reached, and the only thing worse than
a half-sent mass email is a half-sent mass email with no record of it.

Delivery is one message per recipient rather than one message with everybody in
the To field, which would publish every customer's address to every customer.
*/
func (s Service) Send(ctx context.Context, actorID, actorEmail, subject, body string) (admin.Announcement, error) {
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)

	recipients, err := s.Admin.Recipients(ctx)
	if err != nil {
		return admin.Announcement{}, err
	}

	record, err := s.Admin.RecordAnnouncement(ctx, actorID, actorEmail, subject, body, len(recipients))
	if err != nil {
		return admin.Announcement{}, err
	}
	if s.Mailer == nil {
		return record, fmt.Errorf("recorded, but no mailer is configured to send it")
	}

	// Delivery happens in the background: a thousand recipients with a pause
	// between each is longer than any HTTP request should be held open for.
	go s.deliver(record, subject, body, recipients)
	return record, nil
}

func (s Service) deliver(record admin.Announcement, subject, body string, recipients []string) {
	pause := s.Pause
	if pause == 0 {
		pause = 120 * time.Millisecond
	}

	failures := 0
	for _, to := range recipients {
		message := mailer.Message{
			To:      to,
			Subject: subject,
			Text:    textBody(body, s.WebURL),
			HTML:    htmlBody(subject, body, s.WebURL),
			Headers: map[string]string{
				"List-Unsubscribe": fmt.Sprintf("<mailto:unsubscribe@%s?subject=unsubscribe>", hostOf(s.WebURL)),
			},
		}
		if err := s.Mailer.Send(message); err != nil {
			failures++
			log.Printf("announcement %s to %s failed: %v", record.ID, to, err)
		}
		time.Sleep(pause)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Admin.NoteFailures(ctx, record.ID, failures); err != nil {
		log.Printf("announcement %s: could not record %d failures: %v", record.ID, failures, err)
	}
	log.Printf("announcement %s sent to %d recipients, %d failed", record.ID, len(recipients), failures)
}

func hostOf(url string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(host, "/:"); i > 0 {
		host = host[:i]
	}
	if host == "" {
		return "envisecrets.com"
	}
	return host
}

func textBody(body, web string) string {
	if strings.TrimSpace(web) == "" {
		web = "https://envisecrets.com"
	}
	return fmt.Sprintf("%s\n\n—\nEnvi\n%s\n\nReply to this email to reach us, or reply with "+
		"\"unsubscribe\" to stop receiving these.\n", body, web)
}

/*
htmlBody wraps the written body in the product's palette.

The body is escaped and then has its paragraph breaks restored, rather than
being passed through as markup. An announcement is written in a textarea by a
person, and treating what they typed as HTML would make a stray angle bracket
break the email and a pasted script tag something worse.
*/
func htmlBody(subject, body, web string) string {
	if strings.TrimSpace(web) == "" {
		web = "https://envisecrets.com"
	}
	paragraphs := []string{}
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		escaped := strings.ReplaceAll(html.EscapeString(block), "\n", "<br>")
		paragraphs = append(paragraphs, fmt.Sprintf(
			`<p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:#3f3f46;">%s</p>`, escaped))
	}

	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#fafafa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Inter,Roboto,Helvetica,Arial,sans-serif;color:#09090b;-webkit-font-smoothing:antialiased;">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e8e8ea;border-radius:16px;overflow:hidden;">
  <div style="height:4px;background:#f5e73f;"></div>
  <div style="padding:36px 36px 40px;">
    <p style="margin:0 0 28px;font-size:11px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:#a1a1aa;">Envi</p>
    <h1 style="margin:0 0 20px;font-size:24px;line-height:1.3;font-weight:600;letter-spacing:-.02em;color:#09090b;">%s</h1>
    %s
  </div>
  <div style="padding:18px 36px;border-top:1px solid #e8e8ea;background:#fafafa;">
    <p style="margin:0;font-size:12px;line-height:1.6;color:#a1a1aa;">
      You receive these because you have an Envi account at
      <a href="%s" style="color:#71717a;text-decoration:none;">%s</a>.
      Reply with &ldquo;unsubscribe&rdquo; to stop.
    </p>
  </div>
</div></body></html>`,
		html.EscapeString(subject),
		strings.Join(paragraphs, "\n    "),
		html.EscapeString(web), hostOf(web))
}
