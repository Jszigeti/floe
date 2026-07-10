package main

import (
	"fmt"
	"os"

	"github.com/Jszigeti/floe/internal/cli"
	"github.com/Jszigeti/floe/internal/run"
)

var version = "dev" // overridden by goreleaser ldflags

func main() {
	if err := cli.Root(cli.Deps{Version: version, Runner: run.ExecRunner{}}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "floe:", err)
		os.Exit(1)
	}
}
