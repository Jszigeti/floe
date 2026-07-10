package cli

import "github.com/spf13/cobra"

type Deps struct {
	Version string
}

func Root(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "floe",
		Short:        "Versioned backups into the iCloud storage you already pay for",
		Version:      d.Version,
		SilenceUsage: true,
	}
	return cmd
}
