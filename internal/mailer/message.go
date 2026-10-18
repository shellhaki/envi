package mailer

// Message is one email. Fill in both Text and HTML; text-only mail is more likely to be marked as spam.
type Message struct {
	To, Subject, Text, HTML string
	Headers                 map[string]string // e.g. List-Unsubscribe
}

// Sender delivers a Message.
type Sender interface{ Send(Message) error }
