package cli

// envi clean — remove the local .env once its contents are safely on the
// server, so a pull, edit and push cycle leaves nothing plaintext behind.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Clean deletes the env file, but only once the server already holds exactly
// what it contains. Anything else would destroy the only copy of work that was
// never pushed, which no flag default should ever risk.
//
// force skips that check, for a file the caller knows is stale or unwanted.
func Clean(ctx context.Context, c Client, dir, file string, force bool, out io.Writer) error {
	path := resolveEnvFile(dir, file)
	name := filepath.Base(path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(out, "No %s here. Nothing to clean.\n", name)
		return nil
	} else if err != nil {
		return err
	}

	if !force {
		x, err := loadContext(dir)
		if err != nil {
			return err
		}
		local, err := readEnvFile(path)
		if err != nil {
			return err
		}
		remote, _, err := fetchSnapshot(ctx, c, x.Environment.ID)
		if err != nil {
			return err
		}
		if added, changed, removed := compare(local, remote); len(added)+len(changed)+len(removed) > 0 {
			return errors.New(cleanRefusal(name, added, changed, removed))
		}
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	NewUI(out).Success("Removed %s. Nothing plaintext left on disk.", name)
	return nil
}

// cleanRefusal names what would be lost, so the answer is obvious: push it, or
// say you meant to throw it away.
func cleanRefusal(name string, added, changed, removed []string) string {
	differences := len(added) + len(changed) + len(removed)
	return fmt.Sprintf(
		"%s has %d change%s the server does not have (%s). Run envi push first, or envi clean --force to delete them.",
		name, differences, plural(differences), summarise(added, changed, removed),
	)
}

// summarise keeps the counts on one line, listing the first few keys so the
// message says what rather than only how many.
func summarise(added, changed, removed []string) string {
	parts := []string{}
	for _, group := range []struct {
		label string
		keys  []string
	}{{"added", added}, {"changed", changed}, {"removed", removed}} {
		if len(group.keys) == 0 {
			continue
		}
		shown := group.keys
		suffix := ""
		if len(shown) > 3 {
			shown, suffix = shown[:3], fmt.Sprintf(" and %d more", len(group.keys)-3)
		}
		parts = append(parts, fmt.Sprintf("%s %s%s", group.label, joinKeys(shown), suffix))
	}
	return joinParts(parts)
}

func joinKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}
