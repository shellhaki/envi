package otp

import (
	"fmt"
	"net/smtp"
	"strings"
)

type Mailer interface{ Send(string, string) error }
type Gmail struct{ Email, Password string }

func (g Gmail) Send(to, code string) error {
	body := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Envi login code\r\n\r\nYour Envi code is %s. It expires soon.", g.Email, to, code))
	password := strings.ReplaceAll(g.Password, " ", "")
	return smtp.SendMail("smtp.gmail.com:587", smtp.PlainAuth("", g.Email, password, "smtp.gmail.com"), g.Email, []string{to}, body)
}
