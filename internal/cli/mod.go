package cli

// envi mod — edit an environment's secrets in place, with nothing written to
// disk. Pull, edit, push, without the file in the middle.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	projectctx "shellhaki/envi/internal/cli/project"
)

// Mod fetches the current secrets, opens them in the editor, and pushes back
// when asked. The values exist in memory and on the screen, never in a file.
//
// ready is called once the fetch is done, before the screen is taken over, so a
// caller animating a spinner can stop it first.
func Mod(ctx context.Context, c Client, dir, originName string, ready func()) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errNotATerminal
	}

	x, err := loadContext(dir)
	if err != nil {
		return err
	}
	envID, envName := x.Environment.ID, x.Environment.Name
	if originName != "" {
		origins, err := listOrigins(ctx, c, x.Project.ID)
		if err != nil {
			return err
		}
		target, err := findOrigin(origins, originName)
		if err != nil {
			return err
		}
		envID, envName = target.ID, target.Name
	}

	values, revision, err := fetchSnapshot(ctx, c, envID)
	if err != nil {
		return err
	}
	if ready != nil {
		ready()
	}

	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("could not take over the terminal: %w", err)
	}
	// Restore on every exit, including a panic: leaving a terminal in raw mode
	// makes the shell afterwards unusable.
	defer func() {
		_ = term.Restore(fd, state)
		fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[2J\x1b[H") // cursor back, clear
	}()

	width, height := terminalSize(fd, term.GetSize)
	title := x.Project.Name + " · " + envName
	ed := newEditor(secretsAsText(values), title, os.Stdout, width, height)

	for {
		result, err := ed.run(os.Stdin, func() bool { return confirmRaw(os.Stdin, os.Stdout, "Discard changes? [y/N] ") })
		if err != nil {
			return err
		}
		if result == editorQuit {
			return nil
		}

		edited, err := parseEditorText(ed.text())
		if err != nil {
			ed.status = "Not pushed: " + err.Error()
			continue
		}
		newRevision, err := pushSnapshot(ctx, c, envID, edited, revision)
		if err != nil {
			ed.status = "Not pushed: " + pushFailure(err)
			continue
		}
		revision = newRevision
		ed.dirty = false
		ed.status = fmt.Sprintf("Pushed %d secret%s to %s.", len(edited), plural(len(edited)), envName)

		// Keep envi.toml's revision in step so a later envi push from this
		// directory is not rejected for being behind.
		if envID == x.Environment.ID {
			x.Environment.Revision = newRevision
			_ = projectctx.Write(dir, x)
		}
	}
}

// pushSnapshot writes the whole set, guarded by the revision it was read at.
func pushSnapshot(ctx context.Context, c Client, envID string, values map[string]string, expected int64) (int64, error) {
	var result struct {
		Revision int64 `json:"revision"`
	}
	body := map[string]any{"values": values, "expected_revision": expected}
	if err := c.Do(ctx, "PUT", "/environments/"+envID+"/secrets/snapshot", body, &result); err != nil {
		return 0, err
	}
	return result.Revision, nil
}

// pushFailure keeps the message on one line, since it has to fit a status bar.
func pushFailure(err error) string {
	var api *APIError
	if errors.As(err, &api) && api.Code == "stale_revision" {
		return "someone else changed this environment; press ^X and run envi mod again"
	}
	return err.Error()
}

// confirmRaw asks a yes/no question while the terminal is in raw mode, where
// the usual line-buffered prompt would never see a newline.
func confirmRaw(in io.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "\x1b[%dH\x1b[2K\x1b[7m%s\x1b[0m", 999, question)
	buf := make([]byte, 4)
	n, err := in.Read(buf)
	if err != nil || n == 0 {
		return false
	}
	return buf[0] == 'y' || buf[0] == 'Y'
}
