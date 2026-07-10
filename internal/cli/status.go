package cli

import (
	"fmt"

	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/spf13/cobra"
)

func newStatusCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show backup health: snapshots, repo size, schedule, iCloud state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client, cfg, err := buildClient(ctx, d.Runner)
			if err != nil {
				return err
			}
			snaps, err := client.Snapshots(ctx)
			if err != nil {
				return err
			}
			plural := "s"
			if len(snaps) == 1 {
				plural = ""
			}
			cmd.Printf("Snapshots:   %d snapshot%s", len(snaps), plural)
			if len(snaps) > 0 {
				last := snaps[len(snaps)-1]
				cmd.Printf(" — latest %s (%s)", last.ShortID, last.Time.Format("2006-01-02 15:04"))
			}
			cmd.Println()
			if size, err := client.RepoSize(ctx); err == nil {
				cmd.Printf("Repo size:   %.1f GB\n", float64(size)/1e9)
			}
			schedule := "NOT SCHEDULED — run `floe init`"
			if launchd.IsLoaded(ctx, d.Runner) {
				schedule = fmt.Sprintf("daily at %02d:00", cfg.Hour)
			}
			cmd.Printf("Schedule:    %s\n", schedule)
			if icloud.IsICloudPath(cfg.Destination) {
				if rem, err := icloud.QuotaRemaining(ctx, d.Runner); err == nil {
					cmd.Printf("iCloud:      %.1f GB quota remaining", float64(rem)/1e9)
					if pending, err := icloud.SyncPending(ctx, d.Runner); err == nil && pending {
						cmd.Print(" — sync in progress")
						cmd.Println()
						cmd.Println("Retention:   deferred — prune is skipped until iCloud finishes syncing")
					} else {
						cmd.Println()
					}
				}
			}
			return nil
		},
	}
}
