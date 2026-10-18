package invitation

import (
	"context"
	"fmt"
	"log"
	"strings"

	"shellhaki/envi/internal/mailer"
)

// sendEmail emails the invitation link. A failed send is only logged: the invitation already exists.
func (s Service) sendEmail(ctx context.Context, inv Invitation) {
	if s.Mailer == nil {
		return
	}
	projectName := "a project"
	var name string
	if s.DB.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, inv.ProjectID).Scan(&name) == nil && name != "" {
		projectName = name
	}
	link := strings.TrimRight(s.WebURL, "/") + "/invite/" + inv.Token
	if strings.Contains(s.WebURL, "localhost") || strings.Contains(s.WebURL, "127.0.0.1") {
		log.Printf("WARNING: emailing %s an invitation link on %s — it will not resolve for them. Set ENVI_WEB_URL to the public dashboard URL.", inv.Email, s.WebURL)
	}
	expiry := inv.ExpiresAt.Format("Jan 2, 2006 at 3:04 PM MST")

	text := fmt.Sprintf(
		"You've been invited to collaborate on %s on Envi, with %s access.\n\n"+
			"Accept your invitation: %s\n\n"+
			"If you don't have an Envi account yet, this link will let you create one with %s access already waiting.\n\n"+
			"This invitation expires on %s.",
		projectName, inv.Permission, link, inv.Permission, expiry)

	html := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
<h1 style="margin:0 0 16px;font-size:20px;">You&rsquo;re invited to %s</h1>
<p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">You&rsquo;ve been invited to collaborate on <strong>%s</strong> with <strong>%s</strong> access.</p>
<a href="%s" style="display:inline-block;background:#0c111d;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:12px 22px;border-radius:8px;">Accept invitation</a>
<p style="margin:24px 0 0;color:#667085;font-size:13px;line-height:1.6;">Don&rsquo;t have an Envi account yet? This link creates one for you, with access already waiting.</p>
<p style="margin:16px 0 0;color:#98a2b3;font-size:12px;line-height:1.6;">This invitation expires on %s. If the button doesn&rsquo;t work, copy this link:<br><span style="word-break:break-all;">%s</span></p>
</div></body></html>`,
		projectName, projectName, inv.Permission, link, expiry, link)

	message := mailer.Message{To: inv.Email, Subject: fmt.Sprintf("You're invited to %s on Envi", projectName), Text: text, HTML: html}
	if err := s.Mailer.Send(message); err != nil {
		log.Printf("invitation email delivery failed: %v", err)
	}
}
