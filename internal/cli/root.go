package cli

import (
	"github.com/Jszigeti/floe/internal/run"
	"github.com/spf13/cobra"
)

type Deps struct {
	Version  string
	Runner   run.Runner
	AppRoots []string // folders whose apps the backup inventories, none in tests
	CaskURL  string   // Homebrew's cask list, empty in tests
}

func Root(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "floe",
		Short:         "Versioned backups into the iCloud storage you already pay for",
		Version:       d.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newPassphraseCmd(d))
	cmd.AddCommand(newBackupCmd(d))
	cmd.AddCommand(newInitCmd(d))
	cmd.AddCommand(newStatusCmd(d))
	cmd.AddCommand(newSnapshotsCmd(d))
	cmd.AddCommand(newRestoreCmd(d))
	cmd.AddCommand(newDoctorCmd(d))
	cmd.AddCommand(newUninstallCmd(d))
	return cmd
}
