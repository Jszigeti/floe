package cli

import (
	"github.com/Jszigeti/floe/internal/run"
	"github.com/spf13/cobra"
)

type Deps struct {
	Version string
	Runner  run.Runner
}

func Root(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "floe",
		Short:        "Versioned backups into the iCloud storage you already pay for",
		Version:      d.Version,
		SilenceUsage: true,
	}
	cmd.AddCommand(newPassphraseCmd(d))
	cmd.AddCommand(newBackupCmd(d))
	cmd.AddCommand(newInitCmd(d))
	return cmd
}
