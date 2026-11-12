package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const hookUsage = "usage: envi hook zsh|bash|fish|pwsh   (print the hook)\n       envi hook install [shell]        (add it to your shell's startup file)\n       envi hook uninstall              (remove it)"

// runHook handles hook, export, allow, deny and reload.
func (a App) runHook(args []string) int {
	switch args[0] {
	case "hook":
		if len(args) < 2 {
			fmt.Fprintln(a.Err, hookUsage)
			return ExitUsage
		}
		switch args[1] {
		case "install":
			shell := ""
			if len(args) > 2 {
				shell = args[2]
			}
			if err := InstallHook(shell, a.Out); err != nil {
				NewUI(a.Err).Fail("%v", err)
				return ExitConfig
			}
			return ExitOK
		case "uninstall":
			if err := UninstallHook(a.Out); err != nil {
				NewUI(a.Err).Fail("%v", err)
				return ExitConfig
			}
			return ExitOK
		}
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return ExitConfig
		}
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved
		}
		script, err := HookScript(args[1], exe)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			fmt.Fprintln(a.Err, hookUsage)
			return ExitUsage
		}
		fmt.Fprint(a.Out, script)
		return ExitOK

	case "export":
		// Called by the hook before every prompt.
		if len(args) < 2 {
			return ExitUsage
		}
		shell, err := normaliseShell(args[1])
		if err != nil {
			return ExitUsage
		}
		dir, err := os.Getwd()
		if err != nil {
			return ExitOK
		}
		d := exportDeps{getenv: os.Getenv, epoch: readEpoch(), trust: map[string]Allowed{},
			fetch: a.hookFetch}
		if store, err := defaultTrustStore(); err == nil {
			if all, err := store.load(); err == nil {
				d.trust = all
			}
		}
		script, notes := Export(context.Background(), shell, dir, d)
		fmt.Fprint(a.Out, script)
		ui := NewUI(a.Err)
		for _, n := range notes {
			fmt.Fprintln(a.Err, ui.Dim(n))
		}
		return ExitOK

	case "allow":
		fs := flag.NewFlagSet("allow", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		production := fs.Bool("production", false, "allow a production environment to load automatically")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		return a.authenticated("Checking the environment", func(c Client, out io.Writer) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			return AllowHere(context.Background(), c, dir, *production, out)
		})

	case "deny":
		dir, err := os.Getwd()
		if err == nil {
			err = DenyHere(dir, a.Out)
		}
		if err != nil {
			NewUI(a.Err).Fail("%v", err)
			return ExitConfig
		}
		return ExitOK

	default: // reload
		if err := Reload(a.Out); err != nil {
			NewUI(a.Err).Fail("%v", err)
			return ExitConfig
		}
		return ExitOK
	}
}

// hookFetch reads one environment's secrets for the hook.
func (a App) hookFetch(ctx context.Context, envID string) (map[string]string, error) {
	store := a.Store
	if store == nil {
		s, err := defaultTokenStore()
		if err != nil {
			return nil, err
		}
		store = s
	}
	c, err := authorize(a.client(), store)
	if err != nil {
		return nil, err
	}
	values, _, err := fetchSnapshot(ctx, c, envID)
	return values, err
}

// hookTip suggests the hook once, after the first envi init on a machine without it.
func hookTip(out io.Writer) {
	if HookInstalled() {
		return
	}
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	marker := filepath.Join(dir, "hook-tip-shown")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(marker, nil, 0o600)
	ui := NewUI(out)
	ui.Print("")
	ui.Print("%s", ui.Dim(strings.TrimSpace(`
Tip: load these secrets automatically whenever you cd here, no envi run needed:
  envi hook install && envi allow`)))
}
