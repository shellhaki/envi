package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	projectctx "shellhaki/envi/internal/cli/project"
	"strings"
)

const (
	ExitOK        = 0
	ExitUsage     = 2
	ExitAuth      = 3
	ExitForbidden = 4
	ExitConfig    = 5
	ExitAPI       = 6
)

type App struct {
	Out, Err io.Writer
	In       io.Reader
	Version  string
	Store    TokenStore
	Client   Client
}

func (a App) Run(args []string) int {
	if len(args) == 0 {
		a.help()
		return ExitUsage
	}
	switch args[0] {
	case "auth":
		fs := flag.NewFlagSet("auth", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		email := fs.String("email", "", "authenticate with email OTP instead of the browser")
		noBrowser := fs.Bool("no-browser", false, "print the URL instead of opening a browser")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		store, code := a.tokenStore()
		if store == nil {
			return code
		}
		var e error
		if *email != "" {
			e = Authenticate(context.Background(), a.client(), store, a.input(), a.Out, *email)
		} else {
			e = AuthenticateDevice(context.Background(), a.client(), store, a.Out, !*noBrowser)
		}
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitCode(e)
		}
		return ExitOK
	case "logout":
		store, code := a.tokenStore()
		if store == nil {
			return code
		}
		if e := Logout(context.Background(), a.client(), store, a.Out); e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitCode(e)
		}
		return ExitOK
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		name := fs.String("project", "", "project name")
		env := fs.String("env", "", "environment name")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		// init prompts, so it gets the raw writer: a spinner would fight with
		// the questions it asks.
		return a.authenticated("Preparing", func(c Client, _ io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return projectctx.Init(context.Background(), c, a.input(), a.Out, dir, *name, *env)
		})
	case "pull", "push", "diff":
		labels := map[string]string{"pull": "Pulling secrets", "push": "Pushing secrets", "diff": "Comparing with remote"}
		return a.authenticated(labels[args[0]], func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			var count int
			switch args[0] {
			case "pull":
				count, e = Pull(context.Background(), c, dir)
			case "push":
				count, e = Push(context.Background(), c, dir)
			default:
				e = Diff(context.Background(), c, dir, out)
			}
			if e != nil {
				return e
			}
			if args[0] != "diff" {
				ui := NewUI(out)
				ui.Success("%s %d secret%s", map[string]string{"pull": "Pulled", "push": "Pushed"}[args[0]], count, plural(count))
			}
			return nil
		})
	case "project":
		if len(args) < 3 || args[1] != "create" {
			fmt.Fprintln(a.Err, "usage: envi project create <name>")
			return ExitUsage
		}
		return a.authenticated("Creating project", func(c Client, out io.Writer) error {
			return CreateProject(context.Background(), c, args[2], out)
		})
	case "env":
		if len(args) < 3 || args[1] != "create" {
			fmt.Fprintln(a.Err, "usage: envi env create <name> [--project <name>] [--production]")
			return ExitUsage
		}
		fs := flag.NewFlagSet("env create", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		project := fs.String("project", "", "project name (defaults to envi.toml)")
		production := fs.Bool("production", false, "mark as a production environment")
		if e := fs.Parse(args[3:]); e != nil {
			return ExitUsage
		}
		return a.authenticated("Creating environment", func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return CreateEnv(context.Background(), c, dir, *project, args[2], *production, out)
		})
	case "activity":
		fs := flag.NewFlagSet("activity", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		limit := fs.Int("limit", 20, "number of recent events to show")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		return a.authenticated("Loading activity", func(c Client, out io.Writer) error {
			dir, _ := os.Getwd()
			return Activity(context.Background(), c, dir, *limit, out)
		})
	case "token":
		if len(args) < 2 || args[1] != "create" {
			fmt.Fprintln(a.Err, "usage: envi token create --name <name>")
			return ExitUsage
		}
		fs := flag.NewFlagSet("token create", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		name := fs.String("name", "", "token name")
		permission := fs.String("permission", "read", "read, write, or manage")
		ttl := fs.Int("ttl", 0, "lifetime in seconds; 0 means no expiry")
		if e := fs.Parse(args[2:]); e != nil {
			return ExitUsage
		}
		return a.authenticated("Creating token", func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return CreateServiceToken(context.Background(), c, dir, *name, *permission, *ttl, out)
		})
	case "share":
		if len(args) < 2 {
			fmt.Fprintln(a.Err, "usage: envi share <email> [--project <name>] [--env <name>] [--permission <level>]")
			return ExitUsage
		}
		fs := flag.NewFlagSet("share", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		project := fs.String("project", "", "project name")
		env := fs.String("env", "", "environment name")
		permission := fs.String("permission", "read", "read, write, or manage")
		if e := fs.Parse(args[2:]); e != nil {
			return ExitUsage
		}
		return a.authenticated("Sending invitation", func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e == nil {
				e = Share(context.Background(), c, dir, args[1], *project, *env, *permission, out)
			}
			return e
		})
	case "invite":
		if len(args) != 3 || args[1] != "accept" {
			fmt.Fprintln(a.Err, "usage: envi invite accept <token>")
			return ExitUsage
		}
		return a.authenticated("Accepting invitation", func(c Client, _ io.Writer) error { return AcceptInvitation(context.Background(), c, args[2]) })
	case "update":
		ui := NewUI(a.Out)
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		fs := flag.NewFlagSet("update", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		force := fs.Bool("force", false, "update even when already current, or when running a development build")
		if len(args) > 2 {
			if e := fs.Parse(args[2:]); e != nil {
				return ExitUsage
			}
		}
		var e error
		switch sub {
		case "check":
			e = UpdateCheck(context.Background(), ui, a.Version)
		case "now":
			e = UpdateNow(context.Background(), ui, a.Version, *force)
		default:
			fmt.Fprintln(a.Err, "usage: envi update check | envi update now [--force]")
			return ExitUsage
		}
		if e != nil {
			ui := NewUI(a.Err)
			ui.Fail("%v", e)
			return ExitAPI
		}
		return ExitOK
	case "uninstall":
		fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		keep := fs.Bool("keep-config", false, "leave the saved session in place")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		if e := Uninstall(NewUI(a.Out), a.input(), *keep, *yes); e != nil {
			NewUI(a.Err).Fail("%v", e)
			return ExitConfig
		}
		return ExitOK
	case "help", "--help", "-h":
		a.help()
		return ExitOK
	case "version", "--version", "-v":
		fmt.Fprintln(a.Out, a.Version)
		return ExitOK
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n", args[0])
		a.help()
		return ExitUsage
	}
}
func (a App) help() {
	fmt.Fprintln(a.Out, "Usage: envi <command> [flags]\n\nCommands:\n  auth       Authenticate this device in the browser (--email for email OTP)\n  logout     Revoke this device's session\n  project    Create a project (project create <name>)\n  env        Create an environment (env create <name> [--project <name>] [--production])\n  init       Initialize project context\n  pull       Write remote secrets to .env\n  push       Send .env secrets to Envi\n  diff       Compare local and remote keys\n  activity   Show recent activity for your organization\n  share      Invite a project collaborator\n  invite     Accept an invitation\n  token      Manage service tokens\n  update     Check for or install a new version (update check | update now)\n  uninstall  Remove envi from this machine\n  version    Print version\n  help       Show help")
}

// tokenStore resolves the session store, reporting the exit code to use when it
// cannot be opened. A nil store means the caller should return that code.
func (a App) tokenStore() (TokenStore, int) {
	if a.Store != nil {
		return a.Store, ExitOK
	}
	store, e := defaultTokenStore()
	if e != nil {
		fmt.Fprintln(a.Err, e)
		return nil, ExitConfig
	}
	return store, ExitOK
}
func (a App) client() Client {
	c := a.Client
	if c.BaseURL == "" {
		c.BaseURL = LoadConfig().APIURL
	}
	return c
}
func (a App) input() io.Reader {
	if a.In == nil {
		return strings.NewReader("")
	}
	return a.In
}
// authenticated resolves a session and runs a command against it, showing a
// spinner labelled with what is happening. The command is handed the writer to
// print to: the first write stops the spinner, so commands that stream results
// need no knowledge of it, and commands that print only at the end get a clean
// line to print on.
func (a App) authenticated(label string, run func(Client, io.Writer) error) int {
	store, code := a.tokenStore()
	if store == nil {
		return code
	}
	ui := NewUI(a.Out)
	connecting := ui.Spinner("Authenticating")
	c, err := authorize(a.client(), store)
	connecting.Stop()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitAuth
	}
	working := ui.Spinner(label)
	err = run(c, working.Writer(a.Out))
	working.Stop()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitCode(err)
	}
	return ExitOK
}
func ExitCode(err error) int {
	var e *APIError
	if errors.As(err, &e) {
		switch e.Status {
		case 401:
			return ExitAuth
		case 403:
			return ExitForbidden
		default:
			return ExitAPI
		}
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitUsage
	}
	if strings.Contains(err.Error(), "envi.toml") || strings.Contains(err.Error(), "not initialized") {
		return ExitConfig
	}
	return ExitAPI
}
