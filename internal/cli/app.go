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
	"time"
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
		key := fs.String("key", "", "authenticate with a personal API key instead of the browser")
		noBrowser := fs.Bool("no-browser", false, "print the URL instead of opening a browser")
		if e := fs.Parse(args[1:]); e != nil {
			return ExitUsage
		}
		store, code := a.tokenStore()
		if store == nil {
			return code
		}
		var e error
		if *key != "" {
			e = LoginWithKey(context.Background(), a.client(), store, *key, a.Out)
		} else if *email != "" {
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
		// init prompts, so it must print through the spinner's writer: the first
		// prompt stops the animation. Writing to a.Out directly leaves the
		// spinner redrawing over the questions while init waits for input.
		return a.authenticated("Loading projects", func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			return projectctx.Init(context.Background(), c, a.input(), out, dir, *name, *env)
		})
	case "push":
		// envi push [file] [origin <name>] [--force] [--yes]
		file, originName, rest, e := parsePushArgs(args[1:])
		if e != nil {
			fmt.Fprintln(a.Err, e)
			fmt.Fprintln(a.Err, "usage: envi push [file] [origin <name>] [--force] [--yes]")
			return ExitUsage
		}
		fs := flag.NewFlagSet("push", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		force := fs.Bool("force", false, "overwrite the remote instead of requiring a pull first")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if e := fs.Parse(rest); e != nil {
			return ExitUsage
		}
		// Go's flag package stops at the first non-flag word, so
		// "push --force origin prod" would otherwise parse as --force with
		// "origin prod" quietly dropped, and push to the wrong origin.
		if fs.NArg() > 0 {
			fmt.Fprintf(a.Err, "unexpected argument %q\n", fs.Arg(0))
			fmt.Fprintln(a.Err, "usage: envi push [file] [origin <name>] [--force] [--yes]")
			return ExitUsage
		}
		label := "Pushing secrets"
		if originName != "" {
			label = "Reading " + originName
		}
		return a.authenticated(label, func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			// Another origin is one you are not looking at, so that path
			// confirms before writing; the current one is the plain push.
			if originName != "" {
				return PushToOrigin(context.Background(), c, a.input(), out, dir, file, originName, *yes, *force)
			}
			count, e := Push(context.Background(), c, dir, file, *force)
			if e != nil {
				return e
			}
			NewUI(out).Success("Pushed %d secret%s", count, plural(count))
			return nil
		})
	case "run":
		origin, preserve, argv, e := parseRunArgs(args[1:])
		if e != nil {
			fmt.Fprintln(a.Err, e)
			fmt.Fprintln(a.Err, "usage: envi run [--origin <name>] [--preserve-env] -- <command> [args...]")
			return ExitUsage
		}
		if len(argv) == 0 {
			fmt.Fprintln(a.Err, "usage: envi run [--origin <name>] [--preserve-env] -- <command> [args...]")
			return ExitUsage
		}
		c, code := a.session()
		if code != ExitOK {
			return code
		}
		dir, e := os.Getwd()
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return ExitConfig
		}
		// Fetch behind a spinner, which Run stops as soon as the network work
		// is done: it redraws on a timer and would otherwise scribble over the
		// child's output for as long as the child runs. Stop is idempotent, so
		// the second call here only matters on the paths that fail early.
		fetching := NewUI(a.Out).Spinner("Fetching secrets")
		status, e := Run(context.Background(), c, dir, origin, preserve, argv, a.Err, fetching.Stop)
		fetching.Stop()
		if e != nil {
			fmt.Fprintln(a.Err, e)
			// A command that could not be started reports the status a shell
			// would give it, not one of envi's codes.
			var ee *ExecError
			if errors.As(e, &ee) {
				return ee.Status
			}
			return ExitCode(e)
		}
		// The child's status, not envi's: scripts wrapping a command in
		// envi run must see exactly what they would have seen without it.
		return status
	case "key":
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		switch sub {
		case "create":
			fs := flag.NewFlagSet("key create", flag.ContinueOnError)
			fs.SetOutput(a.Err)
			name := fs.String("name", "", "what this key is for, e.g. laptop or ci")
			permission := fs.String("permission", "read", "read, write or manage")
			days := fs.Int("days", 90, "days until the key expires; 0 never expires")
			if e := fs.Parse(args[2:]); e != nil {
				return ExitUsage
			}
			if *name == "" {
				fmt.Fprintln(a.Err, "usage: envi key create --name <name> [--permission read|write|manage] [--days 90]")
				return ExitUsage
			}
			return a.authenticated("Creating API key", func(c Client, out io.Writer) error {
				return CreateAPIKey(context.Background(), c, *name, *permission, time.Duration(*days)*24*time.Hour, out)
			})
		case "list":
			return a.authenticated("Loading API keys", func(c Client, out io.Writer) error {
				return ListAPIKeys(context.Background(), c, out)
			})
		case "revoke":
			if len(args) < 3 {
				fmt.Fprintln(a.Err, "usage: envi key revoke <id>")
				return ExitUsage
			}
			return a.authenticated("Revoking API key", func(c Client, out io.Writer) error {
				return RevokeAPIKey(context.Background(), c, args[2], out)
			})
		default:
			fmt.Fprintln(a.Err, "usage: envi key create --name <name> | envi key list | envi key revoke <id>")
			return ExitUsage
		}
	case "pull", "diff":
		labels := map[string]string{"pull": "Pulling secrets", "diff": "Comparing with remote"}
		return a.authenticated(labels[args[0]], func(c Client, out io.Writer) error {
			dir, e := os.Getwd()
			if e != nil {
				return e
			}
			if args[0] == "diff" {
				return Diff(context.Background(), c, dir, out)
			}
			count, e := Pull(context.Background(), c, dir)
			if e != nil {
				return e
			}
			NewUI(out).Success("Pulled %d secret%s", count, plural(count))
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
	case "origin":
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		switch sub {
		case "list":
			return a.authenticated("Loading origins", func(c Client, out io.Writer) error {
				dir, e := os.Getwd()
				if e != nil {
					return e
				}
				return ListOrigins(context.Background(), c, dir, out)
			})
		case "switch", "use":
			if len(args) < 3 {
				fmt.Fprintln(a.Err, "usage: envi origin switch <name> [--force]")
				return ExitUsage
			}
			fs := flag.NewFlagSet("origin switch", flag.ContinueOnError)
			fs.SetOutput(a.Err)
			force := fs.Bool("force", false, "discard local .env changes instead of refusing")
			if e := fs.Parse(args[3:]); e != nil {
				return ExitUsage
			}
			return a.authenticated("Switching origin", func(c Client, out io.Writer) error {
				dir, e := os.Getwd()
				if e != nil {
					return e
				}
				return SwitchOrigin(context.Background(), c, a.input(), out, dir, args[2], *force)
			})
		case "create":
			if len(args) < 3 {
				fmt.Fprintln(a.Err, "usage: envi origin create <name> [--project <name>] [--production]")
				return ExitUsage
			}
			fs := flag.NewFlagSet("origin create", flag.ContinueOnError)
			fs.SetOutput(a.Err)
			project := fs.String("project", "", "project name (defaults to envi.toml)")
			production := fs.Bool("production", false, "mark as a production origin")
			if e := fs.Parse(args[3:]); e != nil {
				return ExitUsage
			}
			return a.authenticated("Creating origin", func(c Client, out io.Writer) error {
				dir, e := os.Getwd()
				if e != nil {
					return e
				}
				return CreateEnv(context.Background(), c, dir, *project, args[2], *production, out)
			})
		default:
			fmt.Fprintln(a.Err, "usage: envi origin list | envi origin switch <name> | envi origin create <name>")
			return ExitUsage
		}
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
	fmt.Fprintln(a.Out, "Usage: envi <command> [flags]\n\nCommands:\n  auth       Authenticate this device in the browser (--email for a code, --key for an API key)\n  logout     Revoke this device's session\n  key        Personal API keys (key create --name <name> | key list | key revoke <id>)\n  project    Create a project (project create <name>)\n  origin     This project's origins (origin list | origin switch <name> | origin create <name>)\n  env        Alias for origin create\n  init       Initialize project context\n  pull       Write remote secrets to .env\n  push       Send .env secrets to Envi (push [file] [origin <name>] [--force])\n  diff       Compare local and remote keys\n  run        Run a command with the secrets injected, no .env on disk (run -- npm start)\n  activity   Show recent activity for your organization\n  share      Invite a project collaborator\n  invite     Accept an invitation\n  token      Manage service tokens\n  update     Check for or install a new version (update check | update now)\n  uninstall  Remove envi from this machine\n  version    Print version\n  help       Show help")
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
		c.BaseURL = APIURL(a.Version)
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
// session resolves a ready-to-use client, reporting through the spinner and
// returning the exit code to use when it cannot. Split out of authenticated so
// that run, whose exit status belongs to its child process, can get a client
// without inheriting the rest of that wrapper.
func (a App) session() (Client, int) {
	store, code := a.tokenStore()
	if store == nil {
		return Client{}, code
	}
	connecting := NewUI(a.Out).Spinner("Authenticating")
	c, err := authorize(a.client(), store)
	connecting.Stop()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return Client{}, ExitAuth
	}
	return c, ExitOK
}

func (a App) authenticated(label string, run func(Client, io.Writer) error) int {
	c, code := a.session()
	if code != ExitOK {
		return code
	}
	working := NewUI(a.Out).Spinner(label)
	err := run(c, working.Writer(a.Out))
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

// parsePushArgs reads the positional part of
//
//	envi push [file] [origin <name>] [flags]
//
// and hands the rest to the flag set. A leading word is the env file to push
// unless it is "origin" or a flag, which is what lets the file stay optional
// without a --file flag nobody would type.
func parsePushArgs(args []string) (file, origin string, rest []string, err error) {
	rest = args
	if len(rest) > 0 && rest[0] != "origin" && !strings.HasPrefix(rest[0], "-") {
		file, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 && rest[0] == "origin" {
		if len(rest) < 2 || strings.HasPrefix(rest[1], "-") {
			return "", "", nil, errors.New("origin needs a name")
		}
		origin, rest = rest[1], rest[2:]
	}
	return file, origin, rest, nil
}
