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

// Run starts argv with the origin's secrets in its environment and returns its exit status.
func Run(ctx context.Context, c Client, dir, originName string, preserve bool, argv []string, errOut io.Writer, ready func()) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("no command given; usage: envi run [flags] -- <command> [args...]")
	}
	secrets, source, err := secretsToInject(ctx, c, dir, originName)
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
	// Say which origin is being used, on stderr and only for a person at a terminal.
	if ui.Style {
		fmt.Fprintln(errOut, ui.Dim(fmt.Sprintf("%s · %d secret%s injected", source, injected, plural(injected))))
	}
	return execute(argv, env)
}

// secretsToInject fetches the secrets to inject and describes where they came from.
func secretsToInject(ctx context.Context, c Client, dir, originName string) (map[string]string, string, error) {
	x, err := loadContext(dir)
	if err == nil {
		envID, envName := x.Environment.ID, x.Environment.Name
		if originName != "" {
			origins, err := listOrigins(ctx, c, x.Project.ID)
			if err != nil {
				return nil, "", err
			}
			target, err := findOrigin(origins, originName)
			if err != nil {
				return nil, "", err
			}
			envID, envName = target.ID, target.Name
		}
		secrets, _, err := fetchSnapshot(ctx, c, envID)
		return secrets, x.Project.Name + " · " + envName, err
	}

	// No envi.toml.
	project := strings.TrimSpace(os.Getenv("ENVI_PROJECT"))
	environment := strings.TrimSpace(os.Getenv("ENVI_ENVIRONMENT"))
	if originName != "" {
		environment = originName
	}
	return fetchValues(ctx, c, project, environment)
}

// composeEnv lays the fetched secrets over the environment envi itself was given.
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

// validEnvName rejects '=' and null bytes, which break an environment variable.
func validEnvName(k string) bool {
	return k != "" && !strings.ContainsAny(k, "=\x00")
}

// ExecError means the child never started. Status is what a shell would report (126 or 127).
type ExecError struct {
	Status int
	Err    error
}

func (e *ExecError) Error() string { return e.Err.Error() }
func (e *ExecError) Unwrap() error { return e.Err }

// forwarded are the signals a supervisor or a terminal is likely to send.
var forwarded = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

func execute(argv []string, env []string) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	// Give the child our real terminal, so colours and prompts work as normal.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Start(); err != nil {
		// A shell reports 127 for a command it cannot find and 126 for one it can find but cannot execute.
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
				// Windows refuses most signals; nothing to do about it.
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

// exitStatus reports the child's status the way a shell would.
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
