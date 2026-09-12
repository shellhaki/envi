package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// Client sends mail through Gmail's SMTP submission port using implicit TLS.
type Client struct {
	Email    string
	Password string
}

const mimeBoundary = "envi-mail-boundary"

func (g Client) Send(m Message) error {
	return withRetry(func() error { return g.send(m) })
}

func (g Client) send(m Message) error {
	password := strings.ReplaceAll(g.Password, " ", "")

	// Port 465 uses TLS immediately (implicit TLS).
	tlsConfig := &tls.Config{ServerName: "smtp.gmail.com"}

	dialer := &tls.Dialer{Config: tlsConfig}
	conn, err := dialer.Dial("tcp", "smtp.gmail.com:465")
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, "smtp.gmail.com")
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", g.Email, password, "smtp.gmail.com")
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	if err := client.Mail(g.Email); err != nil {
		return fmt.Errorf("set sender: %w", err)
	}
	if err := client.Rcpt(m.To); err != nil {
		return fmt.Errorf("set recipient: %w", err)
	}

	// multipart/alternative with both a text and an HTML part: a bare
	// text-only message is itself a signal spam filters weigh against a
	// sender, so this always carries both, same as the Resend path.
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
			"Content-Type: multipart/alternative; boundary=%s\r\n\r\n"+
			"--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n\r\n"+
			"--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n\r\n"+
			"--%s--\r\n",
		g.Email, m.To, m.Subject, mimeBoundary,
		mimeBoundary, m.Text,
		mimeBoundary, m.HTML,
		mimeBoundary,
	)

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open message: %w", err)
	}
	if _, err := writer.Write([]byte(msg)); err != nil {
		writer.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close message: %w", err)
	}

	return client.Quit()
}
