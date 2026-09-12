package otp

import (
	"fmt"

	"shellhaki/envi/internal/mailer"
)

type Mailer interface {
	Send(string, string) error
}

func codeMessage(to, code string) mailer.Message {
	text := fmt.Sprintf("Your Envi code is %s. It expires soon.", code)
	html := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
<h1 style="margin:0 0 16px;font-size:20px;">Your login code</h1>
<p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">Enter this code to sign in. It expires in a few minutes.</p>
<div style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:32px;font-weight:700;letter-spacing:.14em;background:#f2f4f7;border-radius:8px;padding:16px 24px;text-align:center;">%s</div>
<p style="margin:24px 0 0;color:#98a2b3;font-size:12px;line-height:1.5;">If you didn't request this, you can safely ignore this email — no one can sign in without the code above.</p>
</div></body></html>`, code)
	return mailer.Message{To: to, Subject: "Envi login code", Text: text, HTML: html}
}

type Gmail struct {
	Email    string
	Password string
}

func (g Gmail) Send(to, code string) error {
	return mailer.Client{Email: g.Email, Password: g.Password}.Send(codeMessage(to, code))
}

// Resend delivers OTP codes through the Resend HTTP API instead of Gmail SMTP.
type Resend struct{ Client mailer.Resend }

func (r Resend) Send(to, code string) error {
	return r.Client.Send(codeMessage(to, code))
}
