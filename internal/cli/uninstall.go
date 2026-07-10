package cli

import (
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/spf13/cobra"
)

func newUninstallCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the daily backup schedule (keeps your data)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := launchd.Uninstall(cmd.Context(), d.Runner); err != nil {
				return err
			}
			cmd.Println("✓ Daily backup schedule removed.")
			cmd.Println("Your backups, config, passphrase and the restic binary are untouched.")
			cmd.Println("Full removal: delete ~/.config/floe, ~/.local/share/floe, the Floe.backup folder in your destination, and the keychain entry (security delete-generic-password -s floe -a restic).")
			return nil
		},
	}
}
