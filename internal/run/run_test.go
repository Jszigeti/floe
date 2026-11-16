package run

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerCapturesStdout(t *testing.T) {
	out, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "echo", Args: []string{"hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("out = %q", out)
	}
}

func TestExecRunnerPassesEnv(t *testing.T) {
	out, err := ExecRunner{}.Run(context.Background(),
		Cmd{Name: "sh", Args: []string{"-c", "echo $FLOE_TEST"}, Env: []string{"FLOE_TEST=ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("out = %q", out)
	}
}

func TestExecRunnerStreamsStdout(t *testing.T) {
	var buf bytes.Buffer
	out, err := ExecRunner{}.Run(context.Background(),
		Cmd{Name: "echo", Args: []string{"hi"}, Stdout: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "hi" {
		t.Fatalf("buf = %q", buf.String())
	}
	if out != "" {
		t.Fatalf("out = %q, want nothing captured when streaming", out)
	}
}

func TestExecRunnerErrorIncludesStderr(t *testing.T) {
	_, err := ExecRunner{}.Run(context.Background(),
		Cmd{Name: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want to contain stderr", err)
	}
}

// A child left running in the background keeps the output pipe open after its parent is killed,
// as sudo's mas does once floe gives up on mas: Run must return anyway.
func TestExecRunnerReturnsOnCancelThoughAChildKeepsTheOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "sleep 30 & sleep 30"}})
	if err == nil {
		t.Fatal("a cancelled command must fail")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("Run returned %s after the cancel, the background child held it", took)
	}
}

func TestFakeRecordsAndResponds(t *testing.T) {
	f := &Fake{Responses: map[string]Response{
		"restic init": {Out: "created"},
		"restic bad":  {Err: errors.New("nope")},
	}}
	ctx := context.Background()
	out, err := f.Run(ctx, Cmd{Name: "restic", Args: []string{"init"}})
	if err != nil || out != "created" {
		t.Fatalf("got %q, %v", out, err)
	}
	if _, err := f.Run(ctx, Cmd{Name: "restic", Args: []string{"bad"}}); err == nil {
		t.Fatal("want error")
	}
	if len(f.Calls) != 2 || f.Calls[0].Args[0] != "init" {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestFakeHandlerTakesPrecedence(t *testing.T) {
	f := &Fake{Handler: func(c Cmd) (string, error) { return "handled " + c.Name, nil }}
	out, _ := f.Run(context.Background(), Cmd{Name: "x"})
	if out != "handled x" {
		t.Fatalf("out = %q", out)
	}
}

func TestFakeFallsBackToDefault(t *testing.T) {
	f := &Fake{Default: Response{Out: "default out", Err: errors.New("default err")}}
	out, err := f.Run(context.Background(), Cmd{Name: "unmapped"})
	if out != "default out" || err == nil || err.Error() != "default err" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestCalledWith(t *testing.T) {
	f := &Fake{}
	ctx := context.Background()
	_, _ = f.Run(ctx, Cmd{Name: "restic", Args: []string{"snapshots", "--json"}})
	_, _ = f.Run(ctx, Cmd{Name: "brctl", Args: []string{"status"}})

	if !f.CalledWith("restic", "snapshots") {
		t.Fatal("want CalledWith to match a call containing both tokens")
	}
	if !f.CalledWith("brctl") {
		t.Fatal("want CalledWith to match a single-token call")
	}
	if f.CalledWith("restic", "nope") {
		t.Fatal("want CalledWith false when a token is missing from every call")
	}
	if f.CalledWith("nonexistent") {
		t.Fatal("want CalledWith false when no call matches at all")
	}
}

func TestExecRunnerTerminalHandsOverStdinAndShowsOutput(t *testing.T) {
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.WriteString("hi\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	shown, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, shown
	t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut })

	out, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "read x; echo got $x"}, Terminal: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "got hi" {
		t.Fatalf("captured = %q", out)
	}
	if data, err := os.ReadFile(shown.Name()); err != nil || strings.TrimSpace(string(data)) != "got hi" {
		t.Fatalf("shown = %q, %v", data, err)
	}
}

func TestExecRunnerSucceedsThoughAChildKeepsTheOutput(t *testing.T) {
	out, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo done; sleep 30 &"}})
	if err != nil || out != "done\n" {
		t.Fatalf("got %q, %v, want the command's own success", out, err)
	}
}
