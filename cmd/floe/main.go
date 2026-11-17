package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jszigeti/floe/internal/askpass"
	"github.com/Jszigeti/floe/internal/cli"
	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/run"
)

var version = "dev" // overridden by goreleaser ldflags

func main() {
	if socket := os.Getenv(askpass.SocketEnv); socket != "" {
		if askpass.Answer(socket, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := icloud.AllowDatalessDownloads(); err != nil {
		fmt.Fprintln(os.Stderr, "floe: cannot enable iCloud downloads:", err)
	}
	// FLOE_APPS lets the end-to-end tests scan a folder of their own instead of this Mac's apps.
	roots := []string{cmp.Or(os.Getenv("FLOE_APPS"), "/Applications")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	if err := cli.Root(cli.Deps{Version: version, Runner: run.ExecRunner{}, AppRoots: roots, CaskURL: inventory.CaskURL}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "floe:", err)
		os.Exit(1)
	}
}
