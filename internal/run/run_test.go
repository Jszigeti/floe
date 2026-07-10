package run

import (
	"context"
	"errors"
	"strings"
	"testing"
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

func TestExecRunnerErrorIncludesStderr(t *testing.T) {
	_, err := ExecRunner{}.Run(context.Background(),
		Cmd{Name: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want to contain stderr", err)
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
