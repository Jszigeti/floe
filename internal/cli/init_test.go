package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

func TestInitWizardEndToEnd(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	// pre-install fake restic so EnsureBinary skips the network
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	// keychain lookup fails → wizard must generate and store a new passphrase
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" { // IsInitialized → false
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	// answers: destination, exclusions (none), hour (default)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}

	cfg, err := config.Load()
	if err != nil || cfg.Destination != dest {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	if !f.CalledWith("security", "add-generic-password") {
		t.Fatal("passphrase not stored in keychain")
	}
	if !f.CalledWith("restic", "init") && !f.CalledWith("init") {
		t.Fatalf("repo not initialized; calls = %+v", f.Calls)
	}
	if pp, _ := launchd.PlistPath(); !fileExists(pp) {
		t.Fatal("launchagent not installed")
	}
	if !f.CalledWith("backup") {
		t.Fatal("first backup not run")
	}
	if f.CalledWith("backup", "--quiet") {
		t.Fatal("wizard's first backup should stream progress, not run --quiet")
	}
	if !strings.Contains(out.String(), "password manager") {
		t.Fatal("passphrase warning not shown")
	}
	if !strings.Contains(out.String(), "1:00 PM") {
		t.Fatalf("hour not formatted as 12-hour in output:\n%s", out.String())
	}
}

func TestInitFailsWhenHomeUnresolvable(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", "")
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader("\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when HOME is unresolvable (EnsureBinary needs it)")
	}
}

func TestInitRejectsRelativeDestination(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader("relative/dest\n\n\n"))
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error for a relative destination")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("error must mention absolute path: %v", err)
	}
}

func TestInitReusesExistingPassphrase(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	// keychain lookup succeeds → wizard must reuse it, not generate a new one.
	f.Responses = map[string]run.Response{
		"security find-generic-password -s floe -a restic -w": {Out: "existing-pass\n"},
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}
	if f.CalledWith("security", "add-generic-password") {
		t.Fatal("must not overwrite an existing keychain entry")
	}
	if !strings.Contains(out.String(), "Reusing existing passphrase") {
		t.Fatalf("output missing reuse message:\n%s", out.String())
	}
}

func TestInitAbortsWhenKeychainSetFails(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if c.Name == "security" && c.Args[0] == "add-generic-password" {
			return "", errors.New("keychain locked")
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when the keychain write fails")
	}
}

func TestInitPromptScopeExcludesValidPickIgnoresJunk(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	baseline, _ := config.Default()

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	// answers: destination, exclude picks "1,abc,99" (valid pick + junk + out-of-range), hour default
	cmd.SetIn(strings.NewReader(dest + "\n1,abc,99\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scope.Exclude) != len(baseline.Scope.Exclude)+1 {
		t.Fatalf("exclude list = %v, want exactly one entry appended to the default %v", cfg.Scope.Exclude, baseline.Scope.Exclude)
	}
}

func TestInitFailsWhenLaunchdInstallFails(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)
	// Block Library/LaunchAgents with a regular file so launchd.Install fails.
	os.MkdirAll(filepath.Join(home, "Library"), 0o755)
	if err := os.WriteFile(filepath.Join(home, "Library/LaunchAgents"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when launchd.Install fails")
	}
}

func TestInitFailsWhenFirstBackupFails(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", errors.New("disk full")
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when the first backup fails")
	}
}

func TestInitFailsWhenRepoDirCannotBeCreated(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)
	// Block dest/Floe.backup with a regular file so os.MkdirAll(repo, ...) fails.
	if err := os.WriteFile(filepath.Join(dest, "Floe.backup"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when the repo dir cannot be created")
	}
}

func TestInitWrapsResticInitFailure(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist // IsInitialized → false, so client.Init runs
		}
		if len(c.Args) > 0 && c.Args[0] == "init" {
			return "", errors.New("repo init boom")
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "initializing repository") {
		t.Fatalf("err = %v, want wrapped 'initializing repository' error", err)
	}
}

func TestInitFailsWhenConfigSaveFails(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)
	// Make ~/.config/floe read-only so cfg.Save() fails.
	cfgDir := filepath.Join(home, ".config/floe")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(cfgDir, 0o755) })

	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when the config file cannot be written")
	}
}

func TestInitLogsManifestWarningDuringFirstBackup(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" {
			return "", os.ErrNotExist
		}
		if c.Name == "crontab" {
			return "", errors.New("no crontab for user")
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "crontab") {
		t.Fatalf("output missing crontab warning logged through init's Log wiring:\n%s", out.String())
	}
}

func TestInitExpandsTildeDestination(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" { // IsInitialized → false
			return "", os.ErrNotExist
		}
		return "", nil
	}
	os.MkdirAll(filepath.Join(home, "dest"), 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader("~/dest\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}

	cfg, err := config.Load()
	want := filepath.Join(home, "dest")
	if err != nil || cfg.Destination != want {
		t.Fatalf("cfg.Destination = %q, want %q, err = %v", cfg.Destination, want, err)
	}
}

func TestInitRejectsMissingDestination(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" { // IsInitialized → false
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "does-not-exist")

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "existing directory") {
		t.Fatalf("error must mention 'existing directory': %v", err)
	}
	if _, err := config.Load(); err == nil {
		t.Fatal("config must not be saved on rejected destination")
	}
}

func TestInitAbortsOnAmbiguousKeychainError(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	// keychain lookup fails ambiguously (e.g. locked keychain) → wizard must abort, not overwrite
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", errors.New("keychain locked")
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" { // IsInitialized → false
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error, got nil")
	}
	if f.CalledWith("security", "add-generic-password") {
		t.Fatal("must not overwrite keychain entry on ambiguous read")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
