package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newSnapshotsCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "snapshots",
		Short: "List backup history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := buildClient(cmd.Context(), d.Runner)
			if err != nil {
				return err
			}
			snaps, err := client.Snapshots(cmd.Context())
			if err != nil {
				return err
			}
			if len(snaps) == 0 {
				cmd.Println("No snapshots yet — run `floe backup`.")
				return nil
			}
			cmd.Printf("%-10s %-19s %s\n", "ID", "DATE", "PATHS")
			for _, s := range snaps {
				cmd.Printf("%-10s %-19s %s\n", s.ShortID, s.Time.Format("2006-01-02 3:04 PM"), strings.Join(s.Paths, ", "))
			}
			return nil
		},
	}
}
