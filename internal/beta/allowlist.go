// Package beta gates sign-in to a fixed list of email addresses while the
// product is in private beta.
package beta

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Allowlist is loaded once at startup from a JSON file — add an email to the
// file and restart the process to pick it up. No hot reload, no database
// table: this is meant to be small and temporary, not a real user-management
// system.
type Allowlist struct {
	emails map[string]struct{}
}

// Load reads a JSON array of email addresses from path, e.g.:
//
//	["a@example.com", "b@example.com"]
func Load(path string) (*Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read beta allowlist: %w", err)
	}
	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse beta allowlist (expected a JSON array of emails): %w", err)
	}
	set := make(map[string]struct{}, len(raw))
	for _, e := range raw {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			set[e] = struct{}{}
		}
	}
	return &Allowlist{emails: set}, nil
}

// Allowed reports whether email is on the list. A nil Allowlist allows
// everyone — the caller only holds a non-nil Allowlist when beta gating is
// actually turned on.
func (a *Allowlist) Allowed(email string) bool {
	if a == nil {
		return true
	}
	_, ok := a.emails[strings.ToLower(strings.TrimSpace(email))]
	return ok
}
