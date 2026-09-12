package mailer

// Message is an email to send. Callers always fill both Text and HTML: a
// text-only body is itself a signal spam filters weigh against a sender,
// so every message here goes out as proper multipart/alternative mail with
// both parts present, not HTML bolted onto a plain-text sender after the
// fact.
type Message struct {
	To, Subject, Text, HTML string
}

// Sender delivers a Message. Client (Gmail) and Resend both implement it.
type Sender interface{ Send(Message) error }
