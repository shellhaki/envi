package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

// Run fetches the linked origin's secrets and hands them to a child process as
// its environment, returning that child's exit status.
//
// Nothing is written to disk. The secrets exist in the child's environment block
// and nowhere else, for as long as it runs — which is the point of the command:
// `envi pull` leaves a plaintext .env lying around long after the process that
// needed it has gone.
//
// The returned int is meaningful only when the error is nil. An error means the
// child never started, and the caller reports it with envi's own exit codes.
//
// ready is called once the network work is done and before anything is written
// or executed, so a caller animating a spinner can stop it before the child owns
// the terminal. It must be safe to call more than once; the caller is expected to
// call it again on the error paths.
func Run(ctx context.Context, c Client, dir, originName string, preserve bool, argv []string, errOut io.Writer, ready func()) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("no command given; usage: envi run [flags] -- <command> [args...]")
	}
	x, err := loadContext(dir)
	if err != nil {
		return 0, err
	}

	envID, envName := x.Environment.ID, x.Environment.Name
	if originName != "" {
		origins, err := listOrigins(ctx, c, x.Project.ID)
		if err != nil {
			return 0, err
		}
		target, err := findOrigin(origins, originName)
		if err != nil {
			return 0, err
		}
		envID, envName = target.ID, target.Name
	}

	secrets, _, err := fetchSnapshot(ctx, c, envID)
	if err != nil {
		return 0, err
	}

	env, injected, skipped := composeEnv(os.Environ(), secrets, preserve)
	if ready != nil {
		ready()
	}
	ui := NewUI(errOut)
	for _, key := range skipped {
		ui.Warn("Skipped %q: a secret name cannot contain '=' or a null byte.", key)
	}
	// Which origin you are about to run against is worth knowing, but it is not
	// the command's output: stderr only, and only for a human watching.
	if ui.Style {
		fmt.Fprintln(errOut, ui.Dim(fmt.Sprintf("%s · %s · %d secret%s injected",
			x.Project.Name, envName, injected, plural(injected))))
	}
	return execute(argv, env)
}

// composeEnv lays the fetched secrets over the environment envi itself was given.
//
// A secret wins over an inherited value, because the whole point is that Envi is
// where the value lives; preserve flips that, for the case where you deliberately
// set one on the command line. The replacement is done in place rather than by
// appending a second entry for the same name: os/exec does de-duplicate in favour
// of the last value, but relying on that is a silent dependency on an
// implementation detail, and a child that reads its own environ directly would
// see both.
func composeEnv(parent []string, secrets map[string]string, preserve bool) ([]string, int, []string) {
	out := append([]string(nil), parent...)
	at := make(map[string]int, len(parent))
	for i, kv := range out {
		if j := strings.IndexByte(kv, '='); j > 0 {
			at[kv[:j]] = i
		}
	}
	// Sorted so both the resulting block and any warnings are deterministic.
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var skipped []string
	injected := 0
	for _, k := range keys {
		if !validEnvName(k) {
			skipped = append(skipped, k)
			continue
		}
		if i, ok := at[k]; ok {
			if preserve {
				continue
			}
			out[i] = k + "=" + secrets[k]
			injected++
			continue
		}
		out = append(out, k+"="+secrets[k])
		injected++
	}
	return out, injected, skipped
}

// validEnvName rejects the two things that cannot survive an environment block:
// an '=' would quietly define a differently-named variable, and a null byte
// truncates it. Nothing else is policed — the server does not validate key names
// on write, and a name that is merely unusual is still the user's to choose.
func validEnvName(k string) bool {
	return k != "" && !strings.ContainsAny(k, "=\x00")
}

// ExecError is a failure to start the child at all, carrying the exit status a
// shell would report for it. Envi's own exit codes do not apply here: the caller
// asked to run a command, so the answer should be about that command.
type ExecError struct {
	Status int
	Err    error
}

func (e *ExecError) Error() string { return e.Err.Error() }
func (e *ExecError) Unwrap() error { return e.Err }

// forwarded are the signals a supervisor or a terminal is likely to send. They
// are taken over rather than left to Go's default handler, which would kill envi
// on the first Ctrl-C and leave the child orphaned with its status unreported.
var forwarded = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

func execute(argv []string, env []string) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	// The real descriptors, not pipes: the child keeps its terminal, so colour,
	// progress bars, and interactive prompts behave as if envi were not here.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Start(); err != nil {
		// A shell reports 127 for a command it cannot find and 126 for one it
		// can find but cannot execute. Anything wrapping envi run is really
		// wrapping the command, so it should see the same numbers.
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return 0, &ExecError{Status: 127, Err: fmt.Errorf("%s: command not found", argv[0])}
		}
		return 0, &ExecError{Status: 126, Err: fmt.Errorf("%s: %w", argv[0], err)}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, forwarded...)
	defer signal.Stop(sig)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sig:
				// Windows can only deliver Kill this way; a refusal there is
				// expected and there is nothing better to do about it.
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()
	close(done)
	if err != nil && cmd.ProcessState == nil {
		return 0, err
	}
	return exitStatus(cmd.ProcessState), nil
}

// exitStatus reports the child's status the way a shell would, so `envi run`
// is transparent to anything checking $?.
func exitStatus(state *os.ProcessState) int {
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	// A negative code means it was killed by a signal rather than exiting.
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}

// parseRunArgs splits envi's own flags from the command to be run.
//
// Go's flag package stops at the first non-flag word or at a bare "--", and
// strips only that first separator, so `envi run -- npm run dev -- --port 3000`
// hands the inner "--" through untouched.
func parseRunArgs(args []string) (origin string, preserve bool, argv []string, err error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := fs.String("origin", "", "run against another origin instead of the linked one")
	p := fs.Bool("preserve-env", false, "keep the existing value when a name is already set")
	if err = fs.Parse(args); err != nil {
		return "", false, nil, err
	}
	return *o, *p, fs.Args(), nil
}
