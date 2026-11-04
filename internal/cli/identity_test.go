package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

const thisMac = "11111111-2222-3333-4444-555555555555"

// ioregOut is the shape of `ioreg -rd1 -c IOPlatformExpertDevice` on a real Mac.
const ioregOut = `+-o J314sAP  <class IOPlatformExpertDevice, id 0x100000254, registered, matched, active, busy 0 (0 ms), retain 30>
  {
    "IOPlatformSerialNumber" = "C02XXXXXXXXX"
    "IOPlatformUUID" = "11111111-2222-3333-4444-555555555555"
  }
`

// answerIoreg makes f report thisMac; tests that set f.Handler answer ioreg themselves.
func answerIoreg(f *run.Fake) {
	if f.Responses == nil {
		f.Responses = map[string]run.Response{}
	}
	f.Responses["ioreg -rd1 -c IOPlatformExpertDevice"] = run.Response{Out: ioregOut}
}

// freshInitHandler answers a first init on thisMac: no passphrase yet, no repository yet.
func freshInitHandler(c run.Cmd) (string, error) {
	switch {
	case c.Name == "ioreg":
		return ioregOut, nil
	case c.Name == "security" && c.Args[0] == "find-generic-password":
		return "", errKeychainNotFound
	case len(c.Args) > 1 && c.Args[0] == "cat":
		return "", errRepoMissing
	}
	return "", nil
}

func runInit(f *run.Fake, stdin string) (string, error) {
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	return out.String(), err
}

// saveMachine records mac in the config setupHome wrote.
func saveMachine(t *testing.T, mac string) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Machine = mac
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestMachineIDParsesIoreg(t *testing.T) {
	f := &run.Fake{}
	answerIoreg(f)
	got, err := machineID(context.Background(), f)
	if err != nil || got != thisMac {
		t.Fatalf("machineID = %q, %v", got, err)
	}
}

func TestMachineIDFailsWithoutUUID(t *testing.T) {
	if _, err := machineID(context.Background(), &run.Fake{}); err == nil {
		t.Fatal("want an error when ioreg prints no IOPlatformUUID")
	}
}

func TestFirstRunRecordsRepoAndMachine(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	answerIoreg(f)
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	name, err := config.MachineName()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cfg.Destination, "Floe.backup", name); cfg.Repo != want || cfg.Machine != thisMac {
		t.Fatalf("repo = %q, machine = %q, want %q and %q", cfg.Repo, cfg.Machine, want, thisMac)
	}
}

func TestBackupStopsOnConfigFromAnotherMac(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	answerIoreg(f)
	saveMachine(t, "OTHER-MAC")
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"backup"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "another Mac") {
		t.Fatalf("err = %v, want the foreign-config stop", err)
	}
	if !f.CalledWith("osascript", "comes from another Mac") {
		t.Fatal("no notification for the foreign config")
	}
	if f.CalledWith("backup", "--quiet") {
		t.Fatal("a foreign config must never back up")
	}
	if !strings.Contains(out.String(), "backup failed") {
		t.Fatalf("no floe.log failure line, doctor would miss the stop:\n%s", out.String())
	}
}

func TestBackupFailsClosedWhenTheMacIsUnreadable(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f) // no ioreg answer: the hardware UUID cannot be read
	saveMachine(t, thisMac)
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "hardware UUID") {
		t.Fatalf("err = %v, want the unreadable-UUID failure", err)
	}
	if f.CalledWith("backup", "--quiet") {
		t.Fatal("must not back up without knowing which Mac this is")
	}
}

func TestDoctorReportsConfigFromAnotherMac(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	answerIoreg(f)
	saveMachine(t, "OTHER-MAC")
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must fail on a config from another Mac")
	}
	if !strings.Contains(out.String(), "✗ this mac") || !strings.Contains(out.String(), foreignConfigMsg) {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestInitRewritesConfigFromAnotherMac(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	installFakeRestic(t)
	dest := filepath.Join(home, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	old.Destination, old.Machine, old.Repo = dest, "OTHER-MAC", filepath.Join(dest, "Floe.backup", "Old-Mac")
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	out, err := runInit(&run.Fake{Handler: freshInitHandler}, dest+"\n\n\n")
	if err != nil {
		t.Fatalf("init must be the way out of a foreign config: %v\n%s", err, out)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	name, err := config.MachineName()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machine != thisMac || cfg.Repo != filepath.Join(dest, "Floe.backup", name) {
		t.Fatalf("machine = %q, repo = %q", cfg.Machine, cfg.Repo)
	}
}
