package cmd

import (
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

func newRetentionCommand() *cobra.Command {
	command := &cobra.Command{Use: "retention", Short: "Manage expiring source data", Args: cobra.NoArgs}
	var limit int
	sweep := &cobra.Command{Use: "sweep", Short: "Delete expired provider blobs while retaining observation metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		count, err := jobs.SweepExpiredBlobs(cmd.Context(), runtime, limit)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(map[string]any{"expired_blobs": count})
		}
		ui.Success("Expired %d provider blobs", count)
		return nil
	}}
	sweep.Flags().IntVar(&limit, "limit", 500, "Maximum blobs to expire")
	command.AddCommand(sweep)
	return command
}
