package audit

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("forbidden")

type Event struct {
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Metadata   []byte `json:"metadata"`
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) List(ctx context.Context, user, org string) ([]Event, error) {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND org_id=$2 AND role IN('owner','admin'))`, user, org).Scan(&ok)
	if !ok {
		return nil, ErrForbidden
	}
	rows, e := s.DB.Query(ctx, `SELECT action,target_type,COALESCE(target_id::text,''),metadata FROM audit_events WHERE org_id=$1 ORDER BY created_at DESC LIMIT 200`, org)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Action, &v.TargetType, &v.TargetID, &v.Metadata); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
