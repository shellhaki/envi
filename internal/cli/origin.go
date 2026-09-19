package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	projectctx "shellhaki/envi/internal/cli/project"
)

// An origin is one environment of the current project — dev, staging, prod.
// The one you are on lives in envi.toml, and pull and push act on it.

type origin struct {
	ID         string
	Name       string
	Production bool
}

func listOrigins(ctx context.Context, c Client, projectID string) ([]origin, error) {
	var envs []struct {
		ID, ProjectID, Name string
		Production          bool
	}
	if err := c.Do(ctx, "GET", "/projects/"+projectID+"/environments", nil, &envs); err != nil {
		return nil, err
	}
	out := make([]origin, 0, len(envs))
	for _, e := range envs {
		out = append(out, origin{ID: e.ID, Name: e.Name, Production: e.Production})
	}
	return out, nil
}

func findOrigin(origins []origin, name string) (origin, error) {
	for _, o := range origins {
		if strings.EqualFold(o.Name, name) {
			return o, nil
		}
	}
	names := make([]string, 0, len(origins))
	for _, o := range origins {
		names = append(names, o.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return origin{}, fmt.Errorf("no origin named %q; this project has none yet", name)
	}
	return origin{}, fmt.Errorf("no origin named %q; this project has: %s", name, strings.Join(names, ", "))
}

// ListOrigins prints the project's origins, marking the current one.
func ListOrigins(ctx context.Context, c Client, dir string, out io.Writer) error {
	x, err := loadContext(dir)
	if err != nil {
		return err
	}
	origins, err := listOrigins(ctx, c, x.Project.ID)
	if err != nil {
		return err
	}
	ui := NewUI(out)
	for _, o := range origins {
		marker := "  "
		if o.ID == x.Environment.ID {
			marker = ui.Bold("* ")
		}
		label := o.Name
		if o.Production {
			label += ui.Dim("  (production)")
		}
		fmt.Fprintf(out, "%s%s\n", marker, label)
	}
	return nil
}

// SwitchOrigin points envi.toml at another origin and loads its secrets into
// .env, so the working directory always matches the origin it claims to be on.
//
// Local edits that were never pushed would be silently overwritten by that
// pull, so they stop the switch unless the caller forces it.
func SwitchOrigin(ctx context.Context, c Client, in io.Reader, out io.Writer, dir, name string, force bool) error {
	x, err := loadContext(dir)
	if err != nil {
		return err
	}
	origins, err := listOrigins(ctx, c, x.Project.ID)
	if err != nil {
		return err
	}
	target, err := findOrigin(origins, name)
	if err != nil {
		return err
	}
	ui := NewUI(out)
	if target.ID == x.Environment.ID {
		ui.Success("Already on %s", target.Name)
		return nil
	}

	if !force {
		changed, err := localChanges(ctx, c, dir, x.Environment.ID)
		if err != nil {
			return err
		}
		if changed {
			return fmt.Errorf("your .env has changes not pushed to %s; run envi push first, or envi origin switch %s --force to discard them", x.Environment.Name, name)
		}
	}

	x.Environment = projectctx.Resource{ID: target.ID, Name: target.Name}
	if err = projectctx.Write(dir, x); err != nil {
		return err
	}
	// Pull rewrites .env and records the new origin's revision, so the next
	// push compares against the right one.
	n, err := Pull(ctx, c, dir)
	if err != nil {
		return err
	}
	ui.Success("Switched to %s — pulled %d secret%s", ui.Bold(target.Name), n, plural(n))
	if target.Production {
		ui.Warn("%s is a production origin.", target.Name)
	}
	return nil
}

// localChanges reports whether .env differs from what the given origin holds.
func localChanges(ctx context.Context, c Client, dir, envID string) (bool, error) {
	f, err := os.Open(envPath(dir))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	local, err := parseEnv(f)
	if err != nil {
		return false, err
	}
	remote, _, err := fetchSnapshot(ctx, c, envID)
	if err != nil {
		return false, err
	}
	if len(local) != len(remote) {
		return true, nil
	}
	for k, v := range local {
		if rv, ok := remote[k]; !ok || rv != v {
			return true, nil
		}
	}
	return false, nil
}

// PushToOrigin pushes a local env file to an origin other than the current one.
// It is the destructive shape of push — overwriting an origin you are not
// looking at — so it states exactly what will change and waits for a yes, which
// only --yes or --force skips. It writes against the target's own revision,
// read a moment earlier, so it is never rejected for the current origin having
// moved on.
func PushToOrigin(ctx context.Context, c Client, in io.Reader, out io.Writer, dir, file, name string, assumeYes, force bool) error {
	x, err := loadContext(dir)
	if err != nil {
		return err
	}
	origins, err := listOrigins(ctx, c, x.Project.ID)
	if err != nil {
		return err
	}
	target, err := findOrigin(origins, name)
	if err != nil {
		return err
	}

	path := resolveEnvFile(dir, file)
	local, err := readEnvFile(path)
	if err != nil {
		return err
	}
	shown := filepath.Base(path)

	// The target's own revision, not the cached one for the current origin:
	// they count independently.
	remote, revision, err := fetchSnapshot(ctx, c, target.ID)
	if err != nil {
		return err
	}

	added, changed, removed := compare(local, remote)
	ui := NewUI(out)
	if len(added)+len(changed)+len(removed) == 0 {
		ui.Success("%s already matches %s", target.Name, shown)
		return nil
	}

	ui.Print("Pushing %s to %s (you are on %s):", shown, ui.Bold(target.Name), x.Environment.Name)
	report(out, ui, "add", added)
	report(out, ui, "change", changed)
	report(out, ui, "remove", removed)
	if target.Production {
		ui.Warn("%s is a production origin.", target.Name)
	}
	if !assumeYes && !force && !confirmWith(ui, in, fmt.Sprintf("Push to %s?", target.Name)) {
		ui.Print("Cancelled.")
		return nil
	}

	var result struct {
		Revision int64 `json:"revision"`
	}
	body := map[string]any{"values": local, "expected_revision": revision}
	if err = c.Do(ctx, "PUT", "/environments/"+target.ID+"/secrets/snapshot", body, &result); err != nil {
		return withForceHint(err)
	}
	ui.Success("Pushed %d secret%s to %s", len(local), plural(len(local)), ui.Bold(target.Name))
	return nil
}

// confirmWith accepts y or yes in any case. Anything else, including an empty
// line or no stdin at all, is a no.
func confirmWith(ui UI, in io.Reader, prompt string) bool {
	fmt.Fprintf(ui.Out, "%s [y/N] ", prompt)
	if in == nil {
		return false
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func compare(local, remote map[string]string) (added, changed, removed []string) {
	for k, v := range local {
		rv, ok := remote[k]
		switch {
		case !ok:
			added = append(added, k)
		case rv != v:
			changed = append(changed, k)
		}
	}
	for k := range remote {
		if _, ok := local[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return
}

func report(out io.Writer, ui UI, verb string, keys []string) {
	if len(keys) == 0 {
		return
	}
	fmt.Fprintf(out, "  %s %d: %s\n", verb, len(keys), ui.Dim(strings.Join(keys, ", ")))
}
