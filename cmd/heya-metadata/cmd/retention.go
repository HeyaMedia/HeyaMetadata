package cmd

import (
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

func newRetentionCommand() *cobra.Command {
	command := &cobra.Command{Use: "retention", Short: "Manage expiring source data", Args: cobra.NoArgs}
	var limit int
	var grace time.Duration
	sweep := &cobra.Command{Use: "sweep", Short: "Delete expired provider blobs while retaining observation metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		count, err := jobs.SweepExpiredBlobs(cmd.Context(), runtime, limit, grace)
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
	sweep.Flags().DurationVar(&grace, "grace", 24*time.Hour, "Fallback delay after lifecycle expiry")

	var imageLimit int
	var inactiveFor time.Duration
	imageSweep := &cobra.Command{Use: "images", Short: "Evict image bundles that have not been read recently", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		if _, err := images.FlushAccesses(cmd.Context(), runtime, 5000); err != nil {
			return err
		}
		count, err := images.SweepCold(cmd.Context(), runtime, imageLimit, inactiveFor)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(map[string]any{"evicted_images": count, "inactive_for": inactiveFor.String()})
		}
		ui.Success("Evicted %d image bundles inactive for %s", count, inactiveFor)
		return nil
	}}
	imageSweep.Flags().IntVar(&imageLimit, "limit", 500, "Maximum image bundles to evict")
	imageSweep.Flags().DurationVar(&inactiveFor, "inactive", images.ColdAfter, "Minimum time since the last image read")
	command.AddCommand(sweep, imageSweep)
	return command
}
