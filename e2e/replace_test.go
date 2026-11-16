//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const brewShimTmpl = `#!/bin/sh
echo "brew $*" >> "__LOG__"
if [ "$1" = "bundle" ] && [ "$2" = "dump" ]; then
  for a in "$@"; do last="$a"; done
  echo 'brew "jq"' > "$last"
fi
exit 0
`

const masShimTmpl = `#!/bin/sh
echo "mas $*" >> "__LOG__"
if [ "$1" = "list" ]; then
  echo "123456  Fake App  (1.0)"
fi
exit 0
`

// newMacShims installs the usual shims plus brew and mas, so neither Mac touches the runner's own apps.
func newMacShims(t *testing.T, store, log string) string {
	t.Helper()
	dir := t.TempDir()
	setupShims(t, dir, store, log)
	writeShim(t, dir, "brew", strings.ReplaceAll(brewShimTmpl, "__LOG__", log))
	writeShim(t, dir, "mas", strings.ReplaceAll(masShimTmpl, "__LOG__", log))
	return dir
}

// runFloe runs the binary as the user whose home is home.
func runFloe(t *testing.T, home, shims, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "PATH=" + shims + ":" + os.Getenv("PATH")}
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("running floe %v: %v", args, err)
	}
	return out.String(), cmd.ProcessState.ExitCode()
}

// TestE2EReplace plays two accounts: alice backs up, bob's new Mac replaces her from that
// backup under another account name, then continues the same history.
func TestE2EReplace(t *testing.T) {
	origBin := realResticBinPath()
	root := t.TempDir()
	alice, bob := filepath.Join(root, "alice"), filepath.Join(root, "bob")
	dest := filepath.Join(alice, "Library/Mobile Documents/com~apple~CloudDocs")
	for _, dir := range []string{dest, filepath.Join(alice, "Documents"), bob} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seedResticFromRealHome(t, origBin, alice)
	seedResticFromRealHome(t, origBin, bob)
	seedEmptyCaskList(t, alice)
	seedEmptyCaskList(t, bob)
	data := t.TempDir()
	log := filepath.Join(data, "calls.log")
	aliceShims := newMacShims(t, filepath.Join(data, "alice-keychain"), log)
	bobShims := newMacShims(t, filepath.Join(data, "bob-keychain"), log)
	if err := os.WriteFile(filepath.Join(alice, "Documents/notes.txt"), []byte("from alice"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runFloe(t, alice, aliceShims, "\n\n\n", "init")
	if code != 0 {
		t.Fatalf("alice's init failed, exit %d:\n%s", code, out)
	}
	restoreResticToRealHome(t, origBin, alice)
	pass, err := os.ReadFile(filepath.Join(data, "alice-keychain"))
	if err != nil {
		t.Fatal(err)
	}
	repos, err := os.ReadDir(filepath.Join(dest, "Floe.backup"))
	if err != nil || len(repos) != 1 {
		t.Fatalf("want alice's one repository, got %v, %v", repos, err)
	}
	aliceRepo := filepath.Join(dest, "Floe.backup", repos[0].Name())

	// alice saved seconds ago: bob picks Replace and confirms alice's Mac is stopped.
	out, code = runFloe(t, bob, bobShims, dest+"\n1\ny\n"+string(pass)+"\n", "init")
	if code != 0 {
		t.Fatalf("bob's replace failed, exit %d:\n%s", code, out)
	}
	mustContain(t, out, "Found a backup")
	mustContain(t, out, "Restart this Mac")

	got, err := os.ReadFile(filepath.Join(bob, "Documents/notes.txt"))
	if err != nil || string(got) != "from alice" {
		t.Fatalf("bob's notes.txt = %q, %v", got, err)
	}
	cfg, err := os.ReadFile(filepath.Join(bob, ".config/floe/config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(cfg), `repo = "`+aliceRepo+`"`)
	mustContain(t, string(cfg), `paths = ["`+bob+`"]`)
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(calls), "brew bundle install --file "+filepath.Join(bob, ".local/state/floe/recipe/Brewfile"))
	mustContain(t, string(calls), "mas install 123456")

	out, code = runFloe(t, bob, bobShims, "", "backup")
	if code != 0 {
		t.Fatalf("bob's backup failed, exit %d:\n%s", code, out)
	}
	out, code = runFloe(t, bob, bobShims, "", "snapshots")
	if lines := nonEmptyLines(out); code != 0 || len(lines) != 3 {
		t.Fatalf("want header + alice's snapshot + bob's, exit %d:\n%s", code, out)
	}
	if repos, err := os.ReadDir(filepath.Join(dest, "Floe.backup")); err != nil || len(repos) != 1 {
		t.Fatalf("bob must continue alice's repository, not add one: %v, %v", repos, err)
	}
}
