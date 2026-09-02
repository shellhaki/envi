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
	Actor      string          `json:"actor"` // email of the acting user, empty for service tokens
	CreatedAt  time.Time       `json:"created_at"`
	Metadata   json.RawMessage `json:"metadata"`
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) List(ctx context.Context, user, org string) ([]Event, error) {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND org_id=$2 AND role IN('owner','admin'))`, user, org).Scan(&ok)
	if !ok {
		return nil, ErrForbidden
	}
	rows, e := s.DB.Query(ctx, `SELECT a.action,a.target_type,COALESCE(a.target_id::text,''),COALESCE(u.email,''),a.created_at,a.metadata FROM audit_events a LEFT JOIN users u ON u.id=a.actor_id WHERE a.org_id=$1 ORDER BY a.created_at DESC LIMIT 200`, org)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var meta []byte
		if e = rows.Scan(&v.Action, &v.TargetType, &v.TargetID, &v.Actor, &v.CreatedAt, &meta); e != nil {
			return nil, e
		}
		v.Metadata = meta
		out = append(out, v)
	}
	return out, rows.Err()
}
