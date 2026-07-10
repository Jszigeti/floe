package cli

import (
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/spf13/cobra"
)

func newPassphraseCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "passphrase",
		Short: "Print the repository passphrase stored in the macOS Keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pass, err := keychain.Get(cmd.Context(), d.Runner)
			if err != nil {
				return err
			}
			cmd.Println(pass)
			cmd.Println("Store this in your password manager — without it your backup is unreadable.")
			return nil
		},
	}
}
