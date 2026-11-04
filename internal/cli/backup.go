package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/notify"
	"github.com/spf13/cobra"
)

func newBackupCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Run a backup now (hooks, snapshot, retention)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client, cfg, err := buildClient(ctx, d.Runner)
			if err != nil {
				return err
			}
			logf := makeLogger(cmd)
			if err := stopForeignConfig(ctx, d.Runner, cfg); err != nil {
				logf("backup failed: %v", err)
				return err
			}
			logf("backup started")
			err = backup.Run(ctx, backup.Deps{
				Cfg:    cfg,
				Client: client,
				Runner: d.Runner,
				Notify: func(title, msg string) {
					if err := notify.Send(ctx, d.Runner, title, msg); err != nil {
						logf("notification failed: %v", err)
					}
				},
				Now:       time.Now,
				Log:       logf,
				Scheduled: os.Getenv("FLOE_SCHEDULED") == "1",
			})
			if err != nil {
				logf("backup failed: %v", err)
				return err
			}
			logf("backup finished")
			return nil
		},
	}
}

// The LaunchAgent plist redirects stdout and stderr to floe.log — writing the file here would duplicate every line.
func makeLogger(cmd *cobra.Command) func(string, ...any) {
	return func(format string, a ...any) {
		line := fmt.Sprintf("%s "+format, append([]any{time.Now().Format(time.RFC3339)}, a...)...)
		cmd.Println(line)
	}
}
