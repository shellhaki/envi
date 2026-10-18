package cli

// envi copy — put an origin's secrets on the clipboard as .env text.

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

// Copy sends the origin's secrets as .env text to sink.
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

// CopyPicked lets the user pick a key and copies its value.
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

// unknownKey names what is actually there.
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

// WriteClipboard pipes text to whichever clipboard program the machine has.
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

// noClipboardMessage says what to install, or to use --stdout (the answer over SSH).
func noClipboardMessage() string {
	switch runtime.GOOS {
	case "darwin", "windows":
		return "no clipboard program found on this machine; use envi copy --stdout instead"
	default:
		return "no clipboard program found; install xclip, xsel or wl-clipboard, or use envi copy --stdout instead"
	}
}
