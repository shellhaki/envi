package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	projectctx "shellhaki/envi/internal/cli/project"
)

// Activity prints the recent audit log for the caller's organization. The org
// is derived from envi.toml when present, otherwise from the personal account.
func Activity(ctx context.Context, c Client, dir string, limit int, out io.Writer) error {
	orgID, e := resolveOrg(ctx, c, dir)
	if e != nil {
		return e
	}
	var events []struct {
		Action     string    `json:"action"`
		TargetType string    `json:"target_type"`
		TargetID   string    `json:"target_id"`
		Actor      string    `json:"actor"`
		CreatedAt  time.Time `json:"created_at"`
	}
	if e = c.Do(ctx, "GET", "/orgs/"+orgID+"/audit-events", nil, &events); e != nil {
		return e
	}
	if len(events) == 0 {
		fmt.Fprintln(out, "No activity yet.")
		return nil
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tWHO\tACTION\tTARGET")
	for _, ev := range events {
		who := ev.Actor
		if who == "" {
			who = "service token"
		}
		target := ev.TargetType
		if ev.TargetID != "" {
			target += " " + shortID(ev.TargetID)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", relTime(ev.CreatedAt), who, ev.Action, target)
	}
	return w.Flush()
}

// resolveOrg finds the organization to report on: the one that owns the
// initialized project if there is one, else the account's personal org.
func resolveOrg(ctx context.Context, c Client, dir string) (string, error) {
	if x, e := projectctx.Load(dir); e == nil {
		var ps []struct{ ID, OrgID string }
		if e = c.Do(ctx, "GET", "/projects", nil, &ps); e == nil {
			for _, p := range ps {
				if p.ID == x.Project.ID {
					return p.OrgID, nil
				}
			}
		}
	}
	var me struct{ OrganizationID string }
	if e := c.Do(ctx, "GET", "/me", nil, &me); e != nil {
		return "", e
	}
	if me.OrganizationID == "" {
		return "", errors.New("no organization found for this account")
	}
	return me.OrganizationID, nil
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
