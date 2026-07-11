//go:build e2e

// Package e2e drives the real compiled floe binary against a real restic
// repository, standing in for the manual smoke test that has historically
// been run by hand before every release. It never imports internal
// packages: everything goes through the binary's stdin/stdout/exit code,
// exactly as a user would experience it.
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

var binPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "floe-e2e-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(tmp, "floe")
	build := exec.Command("go", "build", "-o", binPath, "../cmd/floe")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(tmp)
		panic("building floe binary: " + err.Error())
	}

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

const securityShimTmpl = `#!/bin/sh
STORE="__STORE__"
case "$1" in
  add-generic-password)
    prev=""
    for arg in "$@"; do
      if [ "$prev" = "-w" ]; then
        printf '%s' "$arg" > "$STORE"
        exit 0
      fi
      prev="$arg"
    done
    exit 1
    ;;
  find-generic-password)
    if [ -f "$STORE" ]; then
      cat "$STORE"
      exit 0
    fi
    echo "security: SecKeychainSearchCopyNext: The specified item could not be found." >&2
    exit 44
    ;;
esac
exit 0
`

const osascriptShimTmpl = `#!/bin/sh
echo "$@" >> "__LOG__"
exit 0
`

const launchctlShim = "#!/bin/sh\nexit 0\n"

// LOW quota (~4.7 GiB) deliberately exercises floe's low-quota warnings.
const brctlShim = `#!/bin/sh
case "$1" in
  quota)
    echo "5000000000 bytes of quota remaining in personal account"
    ;;
  status)
    echo "r:8386 ld{synced-all} up:needs-upload uv:1"
    ;;
esac
exit 0
`

// Optimize Storage ON exercises doctor's warn path.
const defaultsShim = "#!/bin/sh\necho 1\nexit 0\n"

func writeShim(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatalf("writing %s shim: %v", name, err)
	}
}

// setupShims installs fake security/launchctl/osascript/brctl/defaults
// binaries in dir, backed by the given keychain store and notification log
// files, so the e2e run never touches the real Keychain, LaunchAgents,
// Notification Center, or iCloud daemon.
func setupShims(t *testing.T, dir, store, notifyLog string) {
	t.Helper()
	writeShim(t, dir, "security", strings.ReplaceAll(securityShimTmpl, "__STORE__", store))
	writeShim(t, dir, "launchctl", launchctlShim)
	writeShim(t, dir, "osascript", strings.ReplaceAll(osascriptShimTmpl, "__LOG__", notifyLog))
	writeShim(t, dir, "brctl", brctlShim)
	writeShim(t, dir, "defaults", defaultsShim)
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func mustContain(t *testing.T, out, sub string) {
	t.Helper()
	if !strings.Contains(out, sub) {
		t.Fatalf("output does not contain %q:\n%s", sub, out)
	}
}

// TestE2E drives the full CLI surface against a real restic repository, in
// one ordered sequence of subtests that deliberately share state (config,
// keychain, snapshots) the same way a real user's machine would accumulate
// it across `floe init`, daily backups, and `floe doctor` runs.
func TestE2E(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Library/Mobile Documents/com~apple~CloudDocs"), 0o755); err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	workDir := t.TempDir()
	dataDir := t.TempDir() // outside HOME so it's never picked up by the backup itself
	store := filepath.Join(dataDir, "keychain-store")
	notifyLog := filepath.Join(dataDir, "notifications.log")
	setupShims(t, shimDir, store, notifyLog)

	run := func(t *testing.T, stdin string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Dir = workDir
		cmd.Env = []string{
			"HOME=" + home,
			"PATH=" + shimDir + ":" + os.Getenv("PATH"),
		}
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else {
				t.Fatalf("running floe %v: %v", args, err)
			}
		}
		return out.String(), code
	}

	notesPath := filepath.Join(home, "Documents", "notes.txt")
	configPath := filepath.Join(home, ".config/floe/config.toml")

	var id1, id2 string

	t.Run("no_config_errors", func(t *testing.T) {
		out, code := run(t, "", "backup")
		if code == 0 {
			t.Fatalf("want non-zero exit, got 0:\n%s", out)
		}
		mustContain(t, out, "floe init")
		lines := nonEmptyLines(out)
		if len(lines) != 1 {
			t.Fatalf("want exactly one output line (no cobra duplicate), got %d:\n%s", len(lines), out)
		}
	})

	t.Run("init_rejects_missing_destination", func(t *testing.T) {
		out, code := run(t, "~/does-not-exist\n\n\n", "init")
		if code == 0 {
			t.Fatalf("want non-zero exit, got 0:\n%s", out)
		}
		mustContain(t, out, "existing directory")
		if _, err := os.Stat(configPath); err == nil {
			t.Fatal("config.toml must not be created when destination is rejected")
		}
	})

	t.Run("init_full_wizard", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(notesPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(notesPath, []byte("v1"), 0o644); err != nil {
			t.Fatal(err)
		}

		out, code := run(t, "\n\n\n", "init")
		if code != 0 {
			t.Fatalf("init failed, exit %d:\n%s", code, out)
		}
		mustContain(t, out, "password manager")
		mustContain(t, out, "1:00 PM")
		mustContain(t, out, "snapshot")
		mustContain(t, out, "saved")

		info, err := os.Stat(store)
		if err != nil || info.Size() == 0 {
			t.Fatalf("keychain store not populated: err=%v size=%v", err, info)
		}

		manifestPath := filepath.Join(home, ".local/state/floe/manifest/applications.txt")
		if _, err := os.Stat(manifestPath); err != nil {
			t.Fatalf("manifest not written: %v", err)
		}
	})

	t.Run("second_backup_and_history", func(t *testing.T) {
		if err := os.WriteFile(notesPath, []byte("v2"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, "", "backup")
		if code != 0 {
			t.Fatalf("backup failed, exit %d:\n%s", code, out)
		}

		out, code = run(t, "", "snapshots")
		if code != 0 {
			t.Fatalf("snapshots failed, exit %d:\n%s", code, out)
		}
		lines := nonEmptyLines(out)
		if len(lines) != 3 {
			t.Fatalf("want header + 2 snapshot rows, got %d lines:\n%s", len(lines), out)
		}
		mustContain(t, lines[0], "ID")
		id1 = strings.Fields(lines[1])[0]
		id2 = strings.Fields(lines[2])[0]
	})

	t.Run("status_icloud", func(t *testing.T) {
		out, code := run(t, "", "status")
		if code != 0 {
			t.Fatalf("status failed, exit %d:\n%s", code, out)
		}
		mustContain(t, out, "2 snapshots")
		mustContain(t, out, "daily at 1:00 PM")
		mustContain(t, out, "GiB quota remaining")
		mustContain(t, out, "sync in progress")
		mustContain(t, out, "deferred")
	})

	t.Run("restore_versions", func(t *testing.T) {
		tmp1 := t.TempDir()
		out, code := run(t, "", "restore", id1, "--target", tmp1)
		if code != 0 {
			t.Fatalf("restore %s failed, exit %d:\n%s", id1, code, out)
		}
		got, err := os.ReadFile(filepath.Join(tmp1, notesPath))
		if err != nil || string(got) != "v1" {
			t.Fatalf("restored notes.txt = %q, err = %v, want v1", got, err)
		}

		tmp2 := t.TempDir()
		out, code = run(t, "", "restore", "latest", "--target", tmp2)
		if code != 0 {
			t.Fatalf("restore latest failed, exit %d:\n%s", code, out)
		}
		got, err = os.ReadFile(filepath.Join(tmp2, notesPath))
		if err != nil || string(got) != "v2" {
			t.Fatalf("restored notes.txt = %q, err = %v, want v2", got, err)
		}
		_ = id2 // id2 stays only for potential ad-hoc debugging; latest already covers it
	})

	t.Run("restore_bogus_id", func(t *testing.T) {
		out, code := run(t, "", "restore", "zzzzzz")
		if code == 0 {
			t.Fatalf("want non-zero exit restoring a bogus id, got 0:\n%s", out)
		}
	})

	t.Run("doctor_checks", func(t *testing.T) {
		out, code := run(t, "", "doctor")
		if code != 0 {
			t.Fatalf("doctor should exit 0 on warnings only, got %d:\n%s", code, out)
		}
		mustContain(t, out, "full disk access")
		mustContain(t, out, "! optimize mac storage")
		mustContain(t, out, "only 4.7 GiB remaining")
		mustContain(t, out, "all local")
		if strings.Contains(out, "✗") {
			t.Fatalf("doctor output must not contain a failure marker:\n%s", out)
		}
	})

	var storedPass string

	t.Run("passphrase_redisplay", func(t *testing.T) {
		raw, err := os.ReadFile(store)
		if err != nil {
			t.Fatal(err)
		}
		storedPass = strings.TrimSpace(string(raw))

		out, code := run(t, "", "passphrase")
		if code != 0 {
			t.Fatalf("passphrase failed, exit %d:\n%s", code, out)
		}
		mustContain(t, out, storedPass)
		mustContain(t, out, "password manager")
	})

	t.Run("reinit_reuses_passphrase", func(t *testing.T) {
		before, err := os.ReadFile(store)
		if err != nil {
			t.Fatal(err)
		}

		out, code := run(t, "\n\n\n", "init")
		if code != 0 {
			t.Fatalf("reinit failed, exit %d:\n%s", code, out)
		}
		mustContain(t, out, "Reusing existing passphrase")

		after, err := os.ReadFile(store)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("keychain store changed on reinit: before=%q after=%q", before, after)
		}
	})

	t.Run("notifications", func(t *testing.T) {
		log, err := os.ReadFile(notifyLog)
		if err != nil {
			t.Fatal(err)
		}
		content := string(log)
		mustContain(t, content, "backups healthy")
		mustContain(t, content, "iCloud quota low")
		if n := strings.Count(content, "backups healthy"); n != 1 {
			t.Fatalf("want exactly one weekly digest, got %d in log:\n%s", n, content)
		}
	})

	t.Run("uninstall_idempotent", func(t *testing.T) {
		plistPath := filepath.Join(home, "Library/LaunchAgents/com.floe.backup.plist")

		out, code := run(t, "", "uninstall")
		if code != 0 {
			t.Fatalf("uninstall failed, exit %d:\n%s", code, out)
		}
		if _, err := os.Stat(plistPath); err == nil {
			t.Fatal("plist still present after uninstall")
		}

		out, code = run(t, "", "uninstall")
		if code != 0 {
			t.Fatalf("second uninstall should still exit 0, got %d:\n%s", code, out)
		}
	})
}
