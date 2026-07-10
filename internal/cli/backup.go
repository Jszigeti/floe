package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/launchd"
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
			logf("backup started")
			err = backup.Run(ctx, backup.Deps{
				Cfg:    cfg,
				Client: client,
				Runner: d.Runner,
				Notify: func(title, msg string) { notify.Send(ctx, d.Runner, title, msg) },
				Now:    time.Now,
				Log:    logf,
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

func makeLogger(cmd *cobra.Command) func(string, ...any) {
	logPath, err := launchd.LogPath()
	var file *os.File
	if err == nil {
		file, _ = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	return func(format string, a ...any) {
		line := fmt.Sprintf("%s "+format, append([]any{time.Now().Format(time.RFC3339)}, a...)...)
		cmd.Println(line)
		if file != nil {
			fmt.Fprintln(file, line)
		}
	}
}
