package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOldHookIsRemovedOnItsFirstCall(t *testing.T) {
	t.Setenv("ENVI_CONFIG_DIR", t.TempDir())
	home, _ := os.UserHomeDir()
	zshrc := filepath.Join(home, ".zshrc")
	fish := filepath.Join(home, ".config", "fish", "conf.d", "envi.fish")
	block := hookBegin + "\ncommand -v envi >/dev/null 2>&1 && eval \"$(envi hook zsh)\"\n" + hookEnd + "\n"
	if err := os.WriteFile(zshrc, []byte("export PATH=/bin\n\n"+block+"alias ll='ls -l'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fish), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fish, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, _ := ConfigDir()
	allowed := filepath.Join(dir, "allowed.json")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(allowed, []byte("{}"), 0o600)

	var out, errOut bytes.Buffer
	app := App{Out: &out, Err: &errOut}
	if code := app.runOldHook([]string{"hook", "zsh"}); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("the shell would eval %q", out.String())
	}
	if got, _ := os.ReadFile(zshrc); string(got) != "export PATH=/bin\nalias ll='ls -l'\n" {
		t.Fatalf(".zshrc is %q", got)
	}
	if _, err := os.Stat(fish); !os.IsNotExist(err) {
		t.Fatal("envi.fish is still there")
	}
	if _, err := os.Stat(allowed); !os.IsNotExist(err) {
		t.Fatal("allowed.json is still there")
	}
	if !bytes.Contains(errOut.Bytes(), []byte(".zshrc")) {
		t.Fatalf("no note about .zshrc: %q", errOut.String())
	}

	errOut.Reset()
	if code := app.runOldHook([]string{"export", "zsh"}); code != ExitOK || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("export: exit %d, out %q, err %q", code, out.String(), errOut.String())
	}
}
