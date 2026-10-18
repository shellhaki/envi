package audit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("forbidden")

type Event struct {
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Actor      string          `json:"actor"` // email of the user; empty for a service token
	CreatedAt  time.Time       `json:"created_at"`
	Metadata   json.RawMessage `json:"metadata"`
}

type Service struct{ DB *pgxpool.Pool }

// List returns the organization's latest 200 events. Only owners and admins may read it.
func (s Service) List(ctx context.Context, userID, orgID string) ([]Event, error) {
	var isAdmin bool
	_ = s.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM memberships
		   WHERE user_id = $1 AND org_id = $2 AND role IN ('owner', 'admin')
		)`, userID, orgID).Scan(&isAdmin)
	if !isAdmin {
		return nil, ErrForbidden
	}

	rows, err := s.DB.Query(ctx, `
		SELECT a.action, a.target_type, COALESCE(a.target_id::text, ''), COALESCE(u.email, ''), a.created_at, a.metadata
		  FROM audit_events a
		  LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.org_id = $1
		 ORDER BY a.created_at DESC
		 LIMIT 200`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		var metadata []byte
		if err := rows.Scan(&e.Action, &e.TargetType, &e.TargetID, &e.Actor, &e.CreatedAt, &metadata); err != nil {
			return nil, err
		}
		e.Metadata = metadata
		events = append(events, e)
	}
	return events, rows.Err()
}
