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
			if target == "" {
				target = "floe-restore-" + args[0]
			}
			if err := client.Restore(cmd.Context(), args[0], target, include); err != nil {
				return err
			}
			cmd.Println("Restored to", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "destination directory (default ./floe-restore-<id>)")
	return cmd
}
