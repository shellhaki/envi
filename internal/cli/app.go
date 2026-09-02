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
		return a.authenticated(func(c Client) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return projectctx.Init(context.Background(), c, a.input(), a.Out, dir, *name, *env)
		})
	case "pull", "push", "diff":
		return a.authenticated(func(c Client) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			switch args[0] {
			case "pull":
				e = Pull(context.Background(), c, dir)
			case "push":
				e = Push(context.Background(), c, dir)
			default:
				e = Diff(context.Background(), c, dir, a.Out)
			}
			if e != nil {
				return e
			}
			if args[0] != "diff" {
				fmt.Fprintln(a.Out, args[0]+" complete")
			}
			return nil
		})
	case "project":
		if len(args) < 3 || args[1] != "create" {
			fmt.Fprintln(a.Err, "usage: envi project create <name>")
			return ExitUsage
		}
		return a.authenticated(func(c Client) error {
			return CreateProject(context.Background(), c, args[2], a.Out)
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
		return a.authenticated(func(c Client) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return CreateEnv(context.Background(), c, dir, *project, args[2], *production, a.Out)
		})
	case "activity":
		fs := flag.NewFlagSet("activity", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		limit := fs.Int("limit", 20, "number of recent events to show")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		return a.authenticated(func(c Client) error {
			dir, _ := os.Getwd()
			return Activity(context.Background(), c, dir, *limit, a.Out)
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
		return a.authenticated(func(c Client) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return CreateServiceToken(context.Background(), c, dir, *name, *permission, *ttl, a.Out)
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
		return a.authenticated(func(c Client) error {
			dir, e := os.Getwd()
			if e == nil {
				e = Share(context.Background(), c, dir, args[1], *project, *env, *permission, a.Out)
			}
			return e
		})
	case "invite":
		if len(args) != 3 || args[1] != "accept" {
			fmt.Fprintln(a.Err, "usage: envi invite accept <token>")
			return ExitUsage
		}
		return a.authenticated(func(c Client) error { return AcceptInvitation(context.Background(), c, args[2]) })
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
	fmt.Fprintln(a.Out, "Usage: envi <command> [flags]\n\nCommands:\n  auth      Authenticate this device in the browser (--email for email OTP)\n  logout    Revoke this device's session\n  project   Create a project (project create <name>)\n  env       Create an environment (env create <name> [--project <name>] [--production])\n  init      Initialize project context\n  pull      Write remote secrets to .env\n  push      Send .env secrets to Envi\n  diff      Compare local and remote keys\n  activity  Show recent activity for your organization\n  share     Invite a project collaborator\n  invite    Accept an invitation\n  token     Manage service tokens\n  version   Print version\n  help      Show help")
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
func (a App) authenticated(run func(Client) error) int {
	store, code := a.tokenStore()
	if store == nil {
		return code
	}
	c, err := authorize(a.client(), store)
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitAuth
	}
	if err = run(c); err != nil {
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
