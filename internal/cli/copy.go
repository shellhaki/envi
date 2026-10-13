package cli

// envi copy — put an origin's secrets on the clipboard as .env text.
//
// It exists for the platforms envi run cannot reach. Vercel, Netlify and
// Cloudflare Workers build and run your code themselves, so there is no process
// to wrap and no entrypoint to own; their environment-variable screens take
// KEY=VALUE lines pasted in bulk instead. One paste beats one row at a time.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Copy renders the origin's secrets as .env text and hands them to sink,
// returning how many there were and which origin they came from.
//
// The sink is the caller's choice — the system clipboard normally, stdout when
// asked — so this function needs no clipboard on the machine to be testable,
// and over SSH the same command still works by another route.
func Copy(ctx context.Context, c Client, dir, originName string, sink func(string) error) (int, string, error) {
	values, label, err := secretsToInject(ctx, c, dir, originName)
	if err != nil {
		return 0, "", err
	}
	if len(values) == 0 {
		return 0, label, nil
	}
	return len(values), label, sink(renderEnv(values))
}

// CopyOne copies one named secret's value, returning the origin it came from.
//
// The value goes out on its own, with no KEY= in front of it: a single secret
// is copied in order to be pasted into a field that wants only the value — a
// platform's variable form, a connection string box — and a name pasted along
// with it would have to be deleted every time.
func CopyOne(ctx context.Context, c Client, dir, originName, key string, sink func(string) error) (string, error) {
	values, label, err := secretsToInject(ctx, c, dir, originName)
	if err != nil {
		return "", err
	}
	value, ok := values[key]
	if !ok {
		return "", unknownKey(key, label, values)
	}
	return label, sink(value)
}

// CopyPicked shows the origin's keys, copies the value of whichever is chosen,
// and returns that key and the origin.
//
// ready is called once the fetch is done and before the list takes the
// terminal, so a caller animating a spinner can stop it first — the same shape
// envi mod uses.
func CopyPicked(
	ctx context.Context,
	c Client,
	dir, originName string,
	in *os.File,
	out io.Writer,
	sink func(string) error,
	ready func(),
) (string, string, error) {
	values, label, err := secretsToInject(ctx, c, dir, originName)
	if err != nil {
		return "", "", err
	}
	if ready != nil {
		ready()
	}
	if len(values) == 0 {
		return "", label, nil
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	chosen, err := runPicker(in, out, "Copy a secret from "+label, keys)
	if err != nil {
		return "", label, err
	}
	return chosen, label, sink(values[chosen])
}

// unknownKey names what is actually there. A mistyped or misremembered key is
// the likeliest reason to land here, and the list is the fastest correction —
// the same answer findOrigin gives for a wrong origin name.
func unknownKey(key, label string, values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s has no secrets", label)
	}
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)
	shown := names
	suffix := ""
	if len(shown) > 8 {
		shown, suffix = shown[:8], fmt.Sprintf(" and %d more", len(names)-8)
	}
	return fmt.Errorf("%s has no secret named %q. It has: %s%s", label, key, strings.Join(shown, ", "), suffix)
}

// WriteClipboard puts text on the system clipboard by piping it to whichever
// clipboard program the machine has.
//
// Shelling out rather than linking a clipboard library keeps the CLI's
// dependencies where they are, and keeps cross-compilation free of cgo: every
// platform envi ships for already has one of these, or has no clipboard at all,
// in which case nothing a library did would help either.
func WriteClipboard(ctx context.Context, text string) error {
	argv, err := clipboardTool()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return fmt.Errorf("%s: %s", filepath.Base(argv[0]), detail)
		}
		return fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
	}
	return nil
}

// clipboardTool finds a program that owns the clipboard, as a full argv.
//
// Linux has no single answer, so the list is ordered by how specific each one
// is: Termux and Wayland have exactly one tool each, and xclip and xsel are
// interchangeable X11 fallbacks. Looking them up rather than reading
// WAYLAND_DISPLAY or TERMUX_VERSION keeps it honest — what matters is which
// binary is actually installed.
func clipboardTool() ([]string, error) {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		candidates = [][]string{
			{"termux-clipboard-set"},
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		return append([]string{path}, candidate[1:]...), nil
	}
	return nil, errors.New(noClipboardMessage())
}

// noClipboardMessage says what to install, and points at the flag that needs no
// clipboard at all — which is the answer over SSH, where installing xclip would
// only put the secrets on a clipboard nobody can reach.
func noClipboardMessage() string {
	switch runtime.GOOS {
	case "darwin", "windows":
		return "no clipboard program found on this machine; use envi copy --stdout instead"
	default:
		return "no clipboard program found; install xclip, xsel or wl-clipboard, or use envi copy --stdout instead"
	}
}
