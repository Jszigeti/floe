package run

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

type Cmd struct {
	Name string
	Args []string
	Env  []string
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Cmd) (string, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %v: %w: %s", c.Name, c.Args, err, stderr.String())
	}
	return stdout.String(), nil
}
