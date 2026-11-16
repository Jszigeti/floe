package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

type Cmd struct {
	Name   string
	Args   []string
	Env    []string
	Stdout io.Writer // when set, output streams here as it's produced instead of being captured, so a long listing never sits in memory
	// Terminal hands the user's terminal to the command, for an installer that prompts; output is still captured.
	Terminal bool
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Cmd) (string, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	// A child the command left running keeps its output open, so a killed command would never return.
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(), c.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	if c.Stdout != nil {
		cmd.Stdout = c.Stdout
	}
	cmd.Stderr = &stderr
	if c.Terminal {
		cmd.Stdin = os.Stdin
		cmd.Stdout = io.MultiWriter(&stdout, os.Stdout)
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	}
	// ErrWaitDelay alone means the command itself succeeded and only its child still held the output.
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return stdout.String(), fmt.Errorf("%s %v: %w: %s", c.Name, c.Args, err, stderr.String())
	}
	return stdout.String(), nil
}
