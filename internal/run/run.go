package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type Cmd struct {
	Name   string
	Args   []string
	Env    []string
	Stdout io.Writer // when set, output streams here as it's produced, in addition to being captured
	// Terminal hands the user's terminal to the command, for an installer that prompts; output is still captured.
	Terminal bool
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Cmd) (string, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	if c.Stdout != nil {
		cmd.Stdout = io.MultiWriter(&stdout, c.Stdout)
	}
	cmd.Stderr = &stderr
	if c.Terminal {
		cmd.Stdin = os.Stdin
		cmd.Stdout = io.MultiWriter(&stdout, os.Stdout)
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	}
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %v: %w: %s", c.Name, c.Args, err, stderr.String())
	}
	return stdout.String(), nil
}
