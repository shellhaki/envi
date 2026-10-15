package admin

import (
	"context"
	"errors"
	"strings"
	"time"
)

/*
Announcements: one message to everybody who wants to hear from the instance.

Three rules this file is built around.

Subscription is honoured without exception. The recipient query filters on
organizations.is_subscribed and there is no flag, parameter or "important
announcement" path that skips it. A product that keeps secrets has to be the
kind of product that keeps its word about a mailing list too.

Every send is recorded before anybody is emailed, with who sent it and what it
said. A mass email nobody is accountable for is not a feature.

Sending is somebody else's job. This package resolves recipients and keeps the
record; the caller delivers. That keeps the mailer out of a package that is
otherwise about reading the database, and makes the recipient query testable
without a working SMTP connection.
*/

// Announcement is one message that went out.
type Announcement struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body"`
	SentByEmail string    `json:"sent_by_email"`
	Recipients  int       `json:"recipients"`
	Failures    int       `json:"failures"`
	CreatedAt   time.Time `json:"created_at"`
}

var (
	errSubject = errors.New("subject is required")
	errBody    = errors.New("body is required")
)

/*
Recipients returns the addresses an announcement would go to.

One address per person, not per organization: somebody who owns a personal
workspace and belongs to two teams is one human with one inbox, and mailing
them three times is how a sender earns a spam complaint.
*/
func (s Service) Recipients(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT u.email
		  FROM users u
		  JOIN memberships m ON m.user_id = u.id
		  JOIN organizations o ON o.id = m.org_id
		 WHERE o.is_subscribed
		 ORDER BY u.email
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

// RecipientCount is how many an announcement would reach, for showing on the
// form before anybody commits to sending it.
func (s Service) RecipientCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `
		SELECT count(DISTINCT u.email)
		  FROM users u
		  JOIN memberships m ON m.user_id = u.id
		  JOIN organizations o ON o.id = m.org_id
		 WHERE o.is_subscribed
	`).Scan(&n)
	return n, err
}

// RecordAnnouncement writes the record and returns it. It is called before
// delivery, so an announcement that half-sends is still accounted for.
func (s Service) RecordAnnouncement(ctx context.Context, actorID, actorEmail, subject, body string, recipients int) (Announcement, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return Announcement{}, errSubject
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Announcement{}, errBody
	}

	a := Announcement{Subject: subject, Body: body, SentByEmail: actorEmail, Recipients: recipients}
	var sentBy any
	if actorID != "" {
		sentBy = actorID
	}
	err := s.DB.QueryRow(ctx, `
		INSERT INTO announcements (subject, body, sent_by, sent_by_email, recipients)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, subject, body, sentBy, actorEmail, recipients).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

// NoteFailures updates the record once delivery has been attempted.
func (s Service) NoteFailures(ctx context.Context, id string, failures int) error {
	_, err := s.DB.Exec(ctx, `UPDATE announcements SET failures = $2 WHERE id = $1`, id, failures)
	return err
}

// Announcements lists what has been sent, newest first.
func (s Service) Announcements(ctx context.Context, limit int) ([]Announcement, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT to_regclass('public.announcements') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return []Announcement{}, nil
	}

	rows, err := s.DB.Query(ctx, `
		SELECT id, subject, body, sent_by_email, recipients, failures, created_at
		  FROM announcements
		 ORDER BY created_at DESC
		 LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.Subject, &a.Body, &a.SentByEmail,
			&a.Recipients, &a.Failures, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetSubscribed turns announcements on or off for one organization. This is
// what an unsubscribe request writes to.
func (s Service) SetSubscribed(ctx context.Context, orgID string, subscribed bool) error {
	_, err := s.DB.Exec(ctx, `UPDATE organizations SET is_subscribed = $2 WHERE id = $1`, orgID, subscribed)
	return err
}
