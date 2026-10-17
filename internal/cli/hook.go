package cli

// Loading secrets automatically when you cd into a project.
//
// `envi hook <shell>` prints a few lines of shell code that run before every
// prompt. They call `envi export <shell>`, which compares where you are with
// what is already loaded and prints only the commands needed to bring the
// shell up to date: nothing at all on almost every prompt, exports when you
// enter an allowed project, and unsets when you leave it.
//
// What is loaded is tracked in a few ENVI_HOOK_* variables in the shell
// itself, so each shell is independent and nothing about it is written to
// disk. The secrets are never written to disk either.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	projectctx "shellhaki/envi/internal/cli/project"
)

// Shells the hook supports.
var hookShells = []string{"zsh", "bash", "fish", "pwsh"}

const (
	stateDir     = "ENVI_HOOK_DIR"
	stateStamp   = "ENVI_HOOK_STAMP"
	stateKeys    = "ENVI_HOOK_KEYS"
	stateRestore = "ENVI_HOOK_RESTORE"
)

// The hook runs while somebody waits for their prompt. A slow network is
// reported and given up on rather than waited out.
const hookTimeout = 4 * time.Second

func normaliseShell(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "zsh":
		return "zsh", nil
	case "bash":
		return "bash", nil
	case "fish":
		return "fish", nil
	case "pwsh", "powershell", "powershell.exe", "pwsh.exe":
		return "pwsh", nil
	}
	return "", fmt.Errorf("unsupported shell %q; use one of: %s", name, strings.Join(hookShells, ", "))
}

// HookScript is what `envi hook <shell>` prints. exe is the absolute path to
// this binary, so the hook never goes through a package manager's launcher on
// every prompt.
func HookScript(shell, exe string) (string, error) {
	shell, err := normaliseShell(shell)
	if err != nil {
		return "", err
	}
	switch shell {
	case "zsh":
		return fmt.Sprintf(`_envi_hook() {
  trap -- '' INT
  eval "$(%s export zsh)"
  trap - INT
}
typeset -ag precmd_functions
if (( ! ${precmd_functions[(I)_envi_hook]} )); then
  precmd_functions=(_envi_hook $precmd_functions)
fi
`, quotePOSIX(exe)), nil
	case "bash":
		return fmt.Sprintf(`_envi_hook() {
  local previous_exit_status=$?
  trap -- '' INT
  eval "$(%s export bash)"
  trap - INT
  return $previous_exit_status
}
if [[ ";${PROMPT_COMMAND[*]:-};" != *";_envi_hook;"* ]]; then
  if [[ "$(declare -p PROMPT_COMMAND 2>&1)" == "declare -a"* ]]; then
    PROMPT_COMMAND=(_envi_hook "${PROMPT_COMMAND[@]}")
  else
    PROMPT_COMMAND="_envi_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  fi
fi
`, quotePOSIX(exe)), nil
	case "fish":
		return fmt.Sprintf(`function __envi_hook --on-event fish_prompt
    %s export fish | source
end
`, quoteFish(exe)), nil
	default:
		return fmt.Sprintf(`if (-not (Test-Path function:global:__EnviOriginalPrompt)) {
  Copy-Item function:global:prompt function:global:__EnviOriginalPrompt
  function global:prompt {
    $previousExit = $global:LASTEXITCODE
    $script = (& %s export pwsh) -join "`+"`n"+`"
    if ($script) { Invoke-Expression $script }
    $global:LASTEXITCODE = $previousExit
    __EnviOriginalPrompt
  }
}
`, quotePwsh(exe)), nil
	}
}

// --- quoting ----------------------------------------------------------------

var shellName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quotePOSIX(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

func quoteFish(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return "'" + strings.ReplaceAll(v, "'", `\'`) + "'"
}

// PowerShell treats the typographic single quotes as quote marks too, so they
// are doubled along with the plain one.
func quotePwsh(v string) string {
	for _, q := range []string{"'", "‘", "’", "‚", "‛"} {
		v = strings.ReplaceAll(v, q, q+q)
	}
	return "'" + v + "'"
}

func setVar(shell, k, v string) string {
	switch shell {
	case "fish":
		return "set -gx " + k + " " + quoteFish(v)
	case "pwsh":
		return "$env:" + k + " = " + quotePwsh(v)
	default:
		return "export " + k + "=" + quotePOSIX(v)
	}
}

func unsetVar(shell, k string) string {
	switch shell {
	case "fish":
		return "set -e " + k
	case "pwsh":
		return "Remove-Item -LiteralPath Env:" + k + " -ErrorAction SilentlyContinue"
	default:
		return "unset " + k
	}
}

// --- export -----------------------------------------------------------------

// hookEnv is the part of the environment the hook reads and writes.
type hookEnv struct {
	dir, stamp string
	keys       []string
	restore    map[string]string
}

func readHookEnv(getenv func(string) string) hookEnv {
	h := hookEnv{dir: getenv(stateDir), stamp: getenv(stateStamp), restore: map[string]string{}}
	if k := getenv(stateKeys); k != "" {
		h.keys = strings.Split(k, ",")
	}
	if r := getenv(stateRestore); r != "" {
		if b, err := base64.StdEncoding.DecodeString(r); err == nil {
			_ = json.Unmarshal(b, &h.restore)
		}
	}
	return h
}

// exportDeps is everything Export needs from outside, so it can be tested
// without a shell, a server or a home directory.
type exportDeps struct {
	getenv func(string) string
	trust  map[string]Allowed
	epoch  string
	// fetch reads an environment's secrets. Called only when something has
	// to load, so the usual prompt never opens the token store.
	fetch func(ctx context.Context, envID string) (map[string]string, error)
}

// Export returns the shell commands that bring this shell up to date for cwd,
// and the one-line notices to show on stderr.
func Export(ctx context.Context, shell, cwd string, d exportDeps) (string, []string) {
	prev := readHookEnv(d.getenv)
	root := findRoot(cwd)

	// The stamp changes whenever this shell's secrets could be out of date:
	// another environment named, permission given or taken away, `envi
	// reload`, or a push or pull here moving the revision.
	stamp := ""
	if root != "" {
		target, revision := "unreadable", "0"
		if x, err := projectctx.Load(root); err == nil {
			target, revision = trustKey(x), fmt.Sprint(x.Environment.Revision)
		}
		allowed := "no"
		if a, ok := d.trust[root]; ok && a.Hash == target {
			allowed = "yes"
		}
		stamp = target + ":" + allowed + ":" + d.epoch + ":" + revision
	}
	// The common case: same project, nothing changed. No output, no network.
	if root == prev.dir && stamp == prev.stamp {
		return "", nil
	}

	var lines, notes []string
	// Undo whatever the last project set, restoring anything it overwrote.
	for _, k := range prev.keys {
		if old, ok := prev.restore[k]; ok {
			lines = append(lines, setVar(shell, k, old))
		} else {
			lines = append(lines, unsetVar(shell, k))
		}
	}
	for _, k := range []string{stateDir, stateStamp, stateKeys, stateRestore} {
		lines = append(lines, unsetVar(shell, k))
	}
	if root == "" {
		if len(prev.keys) > 0 {
			notes = append(notes, "envi: unloaded")
		}
		return strings.Join(lines, "\n") + "\n", notes
	}

	// From here on, remember this folder and stamp whatever happens, so a
	// failure is reported once rather than on every prompt.
	remember := func(keys []string, restore map[string]string) {
		lines = append(lines, setVar(shell, stateDir, root), setVar(shell, stateStamp, stamp))
		if len(keys) > 0 {
			lines = append(lines, setVar(shell, stateKeys, strings.Join(keys, ",")))
		}
		if len(restore) > 0 {
			b, _ := json.Marshal(restore)
			lines = append(lines, setVar(shell, stateRestore, base64.StdEncoding.EncodeToString(b)))
		}
	}

	x, err := projectctx.Load(root)
	if err != nil {
		notes = append(notes, "envi: "+err.Error())
		remember(nil, nil)
		return strings.Join(lines, "\n") + "\n", notes
	}
	where := x.Project.Name + " · " + x.Environment.Name
	if !strings.Contains(stamp, ":yes:") {
		notes = append(notes, fmt.Sprintf("envi: %s is not allowed here yet. Run `envi allow` to load its secrets.", where))
		remember(nil, nil)
		return strings.Join(lines, "\n") + "\n", notes
	}

	fetchCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	secrets, err := d.fetch(fetchCtx, x.Environment.ID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded) {
			err = errors.New("the server took too long")
		}
		notes = append(notes, fmt.Sprintf("envi: couldn't load %s: %v. Run `envi reload` to try again.", where, err))
		remember(nil, nil)
		return strings.Join(lines, "\n") + "\n", notes
	}

	// What the shell holds once the previous project is undone: the values
	// this project might overwrite, and must put back when you leave.
	base := func(k string) (string, bool) {
		for _, p := range prev.keys {
			if p == k {
				old, ok := prev.restore[k]
				return old, ok
			}
		}
		v := d.getenv(k)
		return v, v != ""
	}

	names := make([]string, 0, len(secrets))
	for k := range secrets {
		names = append(names, k)
	}
	sort.Strings(names)
	var keys []string
	restore := map[string]string{}
	skipped := 0
	for _, k := range names {
		if !shellName.MatchString(k) || strings.HasPrefix(k, "ENVI_HOOK_") {
			skipped++
			continue
		}
		if old, ok := base(k); ok {
			restore[k] = old
		}
		lines = append(lines, setVar(shell, k, secrets[k]))
		keys = append(keys, k)
	}
	remember(keys, restore)

	note := fmt.Sprintf("envi: loaded %d secret%s · %s", len(keys), plural(len(keys)), where)
	if skipped > 0 {
		note += fmt.Sprintf(" (%d skipped: not a valid variable name)", skipped)
	}
	notes = append(notes, note)
	return strings.Join(lines, "\n") + "\n", notes
}

// --- allow, deny, reload ----------------------------------------------------

// AllowHere lets the hook load the project at or above dir. origins is how
// the environment's production flag is found; production needs explicit
// consent because every command run in the folder would get live keys.
func AllowHere(ctx context.Context, c Client, dir string, production bool, out io.Writer) error {
	root := findRoot(dir)
	if root == "" {
		return errors.New("no envi.toml here or in any folder above; run envi init first")
	}
	x, err := projectctx.Load(root)
	if err != nil {
		return err
	}
	origins, err := listOrigins(ctx, c, x.Project.ID)
	if err != nil {
		return err
	}
	isProduction := false
	for _, o := range origins {
		if o.ID == x.Environment.ID {
			isProduction = o.Production
		}
	}
	if isProduction && !production {
		return fmt.Errorf("%s is a production environment. Allowing it means every command you run in %s gets production secrets.\nIf that is what you want: envi allow --production", x.Environment.Name, root)
	}

	hash := trustKey(x)
	store, err := defaultTrustStore()
	if err != nil {
		return err
	}
	all, err := store.load()
	if err != nil {
		return err
	}
	all[root] = Allowed{Hash: hash, Project: x.Project.Name, Environment: x.Environment.Name,
		Production: isProduction, AllowedAt: time.Now().UTC()}
	if err := store.save(all); err != nil {
		return err
	}
	if err := bumpEpoch(); err != nil {
		return err
	}

	ui := NewUI(out)
	ui.Success("Allowed %s · %s in %s", x.Project.Name, x.Environment.Name, root)
	if HookInstalled() {
		ui.Print("%s", ui.Dim("Secrets load at your next prompt in this folder."))
	} else {
		ui.Print("%s", ui.Dim("Turn on automatic loading with: envi hook install"))
	}
	return nil
}

// DenyHere stops the hook loading the project at or above dir.
func DenyHere(dir string, out io.Writer) error {
	root := findRoot(dir)
	if root == "" {
		root = dir
	}
	store, err := defaultTrustStore()
	if err != nil {
		return err
	}
	all, err := store.load()
	if err != nil {
		return err
	}
	ui := NewUI(out)
	if _, ok := all[root]; !ok {
		ui.Print("%s was not allowed.", root)
		return nil
	}
	delete(all, root)
	if err := store.save(all); err != nil {
		return err
	}
	if err := bumpEpoch(); err != nil {
		return err
	}
	ui.Success("Denied %s. Its secrets unload at your next prompt.", root)
	return nil
}

// Reload makes every shell's hook fetch again at its next prompt.
func Reload(out io.Writer) error {
	if err := bumpEpoch(); err != nil {
		return err
	}
	NewUI(out).Success("Secrets reload at your next prompt.")
	return nil
}

// --- installing the hook into a shell's startup file ------------------------

const (
	hookBegin = "# >>> envi hook >>>"
	hookEnd   = "# <<< envi hook <<<"
)

// hookLine is what goes in the startup file: it asks envi for the hook each
// time a shell starts, and does nothing if envi has since been removed.
func hookLine(shell string) string {
	switch shell {
	case "fish":
		return "if command -q envi\n    envi hook fish | source\nend"
	case "pwsh":
		return "if (Get-Command envi -ErrorAction SilentlyContinue) { Invoke-Expression ((& envi hook pwsh) -join \"`n\") }"
	default:
		return fmt.Sprintf("command -v envi >/dev/null 2>&1 && eval \"$(envi hook %s)\"", shell)
	}
}

// detectShell is the shell to install for when none is named.
func detectShell() (string, error) {
	if s := filepath.Base(os.Getenv("SHELL")); s != "" && s != "." {
		if shell, err := normaliseShell(strings.TrimSuffix(s, ".exe")); err == nil {
			return shell, nil
		}
	}
	if os.Getenv("PSModulePath") != "" {
		return "pwsh", nil
	}
	return "", errors.New("couldn't tell which shell you use; name it: envi hook install zsh|bash|fish|pwsh")
}

// rcFiles are the startup files a shell reads, the first being where the hook
// is installed and the rest only checked or cleaned up.
func rcFiles(shell string) []string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		return []string{filepath.Join(dir, ".zshrc")}
	case "bash":
		// macOS Terminal starts bash as a login shell, which reads
		// .bash_profile and not .bashrc.
		if isDarwin() {
			return []string{filepath.Join(home, ".bash_profile"), filepath.Join(home, ".bashrc")}
		}
		return []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}
	case "fish":
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		return []string{filepath.Join(config, "fish", "conf.d", "envi.fish")}
	default:
		return pwshProfiles()
	}
}

// HookInstalled reports whether any shell's startup file carries the hook.
func HookInstalled() bool {
	for _, shell := range hookShells {
		for _, f := range rcFiles(shell) {
			if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), hookBegin) {
				return true
			}
		}
	}
	return false
}

// InstallHook adds the hook to the startup file for shell (detected when
// empty). Running it again changes nothing.
func InstallHook(shell string, out io.Writer) error {
	if shell == "" {
		var err error
		if shell, err = detectShell(); err != nil {
			return err
		}
	}
	shell, err := normaliseShell(shell)
	if err != nil {
		return err
	}
	files := rcFiles(shell)
	if len(files) == 0 {
		return errors.New("couldn't find a profile file for PowerShell")
	}
	// Every PowerShell gets it, since 7 and Windows PowerShell keep separate
	// profiles. So does every bash file on macOS: Terminal starts login shells,
	// which read .bash_profile, while a plain `bash`, tmux and many editors start
	// ones that read only .bashrc. Loading the hook twice in one shell is
	// harmless; it registers itself once. Other shells use one startup file.
	targets := files[:1]
	if shell == "pwsh" || (shell == "bash" && isDarwin()) {
		targets = files
	}
	ui := NewUI(out)
	for _, target := range targets {
		installed, err := appendHook(target, shell)
		if err != nil {
			return err
		}
		if installed {
			ui.Success("Installed the envi hook in %s", target)
		} else {
			ui.Print("Already installed in %s", target)
		}
	}
	ui.Print("%s", ui.Dim("Open a new terminal, then run `envi allow` in a project to load its secrets automatically."))
	return nil
}

func appendHook(target, shell string) (bool, error) {
	existing, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if strings.Contains(string(existing), hookBegin) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	block := hookBegin + "\n" + hookLine(shell) + "\n" + hookEnd + "\n"
	if len(existing) > 0 {
		block = "\n" + block
		if !strings.HasSuffix(string(existing), "\n") {
			block = "\n" + block
		}
	}
	f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(block); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// UninstallHook removes the hook from every startup file that has it.
func UninstallHook(out io.Writer) error {
	ui := NewUI(out)
	removed := 0
	for _, shell := range hookShells {
		for _, f := range rcFiles(shell) {
			b, err := os.ReadFile(f)
			if err != nil || !strings.Contains(string(b), hookBegin) {
				continue
			}
			if shell == "fish" {
				if err := os.Remove(f); err != nil {
					return err
				}
			} else if err := os.WriteFile(f, []byte(withoutHook(string(b))), 0o644); err != nil {
				return err
			}
			ui.Success("Removed the envi hook from %s", f)
			removed++
		}
	}
	if removed == 0 {
		ui.Print("The envi hook isn't installed.")
	}
	return nil
}

// withoutHook cuts the marked block, and the blank line added before it.
func withoutHook(s string) string {
	start := strings.Index(s, hookBegin)
	end := strings.Index(s, hookEnd)
	if start < 0 || end < start {
		return s
	}
	end += len(hookEnd)
	if end < len(s) && s[end] == '\n' {
		end++
	}
	before := strings.TrimRight(s[:start], "\n")
	after := s[end:]
	if before == "" {
		return after
	}
	return before + "\n" + after
}
