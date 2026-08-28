package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	kr "shellhaki/envi/internal/cli/keyring"
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
		email := fs.String("email", "", "email address")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		store := a.Store
		if store == nil {
			store = keyringAdapter{store: kr.New()}
		}
		c := a.Client
		if c.BaseURL == "" {
			c.BaseURL = LoadConfig().APIURL
		}
		in := a.In
		if in == nil {
			in = strings.NewReader("")
		}
		if e := Authenticate(context.Background(), c, store, in, a.Out, *email); e != nil {
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
		store := a.Store
		if store == nil {
			store = keyringAdapter{store: kr.New()}
		}
		token, e := ResolveToken(store)
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitAuth
		}
		c := a.Client
		if c.BaseURL == "" {
			c.BaseURL = LoadConfig().APIURL
		}
		c.Token = token
		dir, e := os.Getwd()
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitConfig
		}
		in := a.In
		if in == nil {
			in = strings.NewReader("")
		}
		if e = projectctx.Init(context.Background(), c, in, a.Out, dir, *name, *env); e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitCode(e)
		}
		return ExitOK
	case "pull", "push", "diff":
		store := a.Store
		if store == nil {
			store = keyringAdapter{store: kr.New()}
		}
		token, e := ResolveToken(store)
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitAuth
		}
		c := a.Client
		if c.BaseURL == "" {
			c.BaseURL = LoadConfig().APIURL
		}
		c.Token = token
		dir, e := os.Getwd()
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitConfig
		}
		if args[0] == "pull" {
			e = Pull(context.Background(), c, dir)
		} else if args[0] == "push" {
			e = Push(context.Background(), c, dir)
		} else {
			e = Diff(context.Background(), c, dir, a.Out)
		}
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitCode(e)
		}
		if args[0] != "diff" {
			fmt.Fprintln(a.Out, args[0]+" complete")
		}
		return ExitOK
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
		store := a.Store
		if store == nil {
			store = keyringAdapter{store: kr.New()}
		}
		token, e := ResolveToken(store)
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitAuth
		}
		c := a.Client
		if c.BaseURL == "" {
			c.BaseURL = LoadConfig().APIURL
		}
		c.Token = token
		dir, e := os.Getwd()
		if e != nil {
			return ExitConfig
		}
		if e = CreateServiceToken(context.Background(), c, dir, *name, *permission, *ttl, a.Out); e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitCode(e)
		}
		return ExitOK
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
	fmt.Fprintln(a.Out, "Usage: envi <command> [flags]\n\nCommands:\n  auth      Authenticate with email OTP\n  init      Initialize project context\n  pull      Write remote secrets to .env\n  push      Send .env secrets to Envi\n  diff      Compare local and remote keys\n  share     Invite a project collaborator\n  invite    Accept an invitation\n  token     Manage service tokens\n  version   Print version\n  help      Show help")
}
func (a App) authenticated(run func(Client) error) int {
	store := a.Store
	if store == nil {
		store = keyringAdapter{store: kr.New()}
	}
	token, err := ResolveToken(store)
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return ExitAuth
	}
	c := a.Client
	if c.BaseURL == "" {
		c.BaseURL = LoadConfig().APIURL
	}
	c.Token = token
	err = run(c)
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
