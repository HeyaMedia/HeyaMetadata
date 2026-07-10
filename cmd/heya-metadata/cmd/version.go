package cmd

import (
	"github.com/HeyaMedia/HeyaMetadata/internal/buildinfo"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := buildinfo.Current()
			if ui.JSONMode {
				return ui.OutputJSON(info)
			}

			ui.Info("Version", info.Version)
			ui.Info("Commit", info.Commit)
			ui.Info("Built", info.BuildDate)
			ui.Info("Go", info.GoVersion)
			ui.Info("Platform", info.Platform)
			return nil
		},
	}
}
