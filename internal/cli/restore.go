package cli

import (
	"github.com/spf13/cobra"
)

func newRestoreCmd(d Deps) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "restore <snapshot-id|latest> [path]",
		Short: "Restore a snapshot (or a single path from it)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := buildClient(cmd.Context(), d.Runner)
			if err != nil {
				return err
			}
			include := ""
			if len(args) == 2 {
				include = args[1]
			}
			dest := target
			if dest == "" {
				dest = "floe-restore-" + args[0]
			}
			id := args[0]
			// restic's own "latest" could be the apps snapshot: latest means the newest home backup.
			if id == "latest" {
				snap, err := client.Latest(cmd.Context(), "home")
				if err != nil {
					return err
				}
				id = snap.ShortID
			}
			if err := client.Restore(cmd.Context(), id, dest, include); err != nil {
				return err
			}
			cmd.Println("Restored to", dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "destination directory (default floe-restore-<id>)")
	return cmd
}
