package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/migrations"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Manage Postgres and River schemas",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := platform.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()
			if err := runtime.DB.Ping(cmd.Context()); err != nil {
				return fmt.Errorf("connect to Postgres: %w", err)
			}

			result, err := migrations.Migrate(cmd.Context(), runtime.DB)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(map[string]any{
					"application_migrations": len(result.Application),
					"river_migrations":       len(result.River.Versions),
				})
			}
			ui.Success("Applied %d application and %d River migrations", len(result.Application), len(result.River.Versions))
			return nil
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show application and River schema status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := platform.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()
			status, err := migrations.AppStatus(cmd.Context(), runtime.DB)
			if err != nil {
				return err
			}
			var riverInstalled bool
			if err := runtime.DB.QueryRow(cmd.Context(), `SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&riverInstalled); err != nil {
				return fmt.Errorf("inspect River schema: %w", err)
			}
			result := map[string]any{
				"application_current": status.Current,
				"application_latest":  status.Latest,
				"application_pending": len(status.Pending),
				"river_installed":     riverInstalled,
			}
			if ui.JSONMode {
				return ui.OutputJSON(result)
			}
			ui.Info("Application", fmt.Sprintf("%d/%d (%d pending)", status.Current, status.Latest, len(status.Pending)))
			ui.Info("River", fmt.Sprintf("installed=%t", riverInstalled))
			return nil
		},
	})
	return command
}

func newWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run durable background workers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			runtime, err := platform.Open(ctx, cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()
			if err := runtime.Ensure(ctx, cfg); err != nil {
				return err
			}
			if err := requireCurrentSchema(ctx, runtime); err != nil {
				return err
			}

			client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, true)
			if err != nil {
				return err
			}
			if err := client.Start(ctx); err != nil {
				return fmt.Errorf("start River workers: %w", err)
			}
			ui.Success("Worker started with %d default slots and %d image slots", cfg.Worker.MaxWorkers, cfg.Worker.ImageMaxWorkers)
			<-ctx.Done()

			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.Stop(stopCtx); err != nil {
				return fmt.Errorf("stop River workers: %w", err)
			}
			return nil
		},
	}
}

func newSmokeCommand() *cobra.Command {
	var wait time.Duration
	command := &cobra.Command{
		Use:   "smoke",
		Short: "Enqueue and verify a Postgres, River, Redis, and S3 round trip",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := platform.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()
			if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
				return err
			}
			if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
				return err
			}

			client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
			if err != nil {
				return err
			}
			nonceBytes := make([]byte, 16)
			if _, err := rand.Read(nonceBytes); err != nil {
				return fmt.Errorf("generate smoke nonce: %w", err)
			}
			inserted, err := client.Insert(cmd.Context(), jobs.PlatformSmokeArgs{
				Nonce: hex.EncodeToString(nonceBytes), RequestedAt: time.Now().UTC(),
			}, &river.InsertOpts{MaxAttempts: 3, Priority: 1})
			if err != nil {
				return fmt.Errorf("enqueue platform smoke job: %w", err)
			}
			if wait <= 0 {
				return outputSmokeQueued(inserted.Job.ID)
			}

			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				var observationID string
				var checksum string
				err := runtime.DB.QueryRow(cmd.Context(), `
                    SELECT observation_id, blob_checksum
                    FROM platform_smoke_runs
                    WHERE river_job_id = $1`, inserted.Job.ID,
				).Scan(&observationID, &checksum)
				if err == nil {
					if ui.JSONMode {
						return ui.OutputJSON(map[string]any{
							"job_id": inserted.Job.ID, "state": "completed",
							"observation_id": observationID, "blob_checksum": checksum,
						})
					}
					ui.Success("Platform smoke job %d completed", inserted.Job.ID)
					ui.Info("Observation", observationID)
					ui.Info("Blob", checksum)
					return nil
				}

				var state rivertype.JobState
				if stateErr := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id = $1`, inserted.Job.ID).Scan(&state); stateErr == nil {
					switch state {
					case rivertype.JobStateCancelled, rivertype.JobStateDiscarded:
						return fmt.Errorf("platform smoke job %d ended in state %s", inserted.Job.ID, state)
					}
				}

				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-deadline.C:
					return fmt.Errorf("timed out after %s waiting for platform smoke job %d", wait, inserted.Job.ID)
				case <-ticker.C:
				}
			}
		},
	}
	command.Flags().DurationVar(&wait, "wait", 20*time.Second, "Wait for the worker to complete the job (0 disables waiting)")
	return command
}

func requireCurrentSchema(ctx context.Context, runtime *platform.Runtime) error {
	status, err := migrations.AppStatus(ctx, runtime.DB)
	if err != nil {
		return err
	}
	if len(status.Pending) > 0 {
		return fmt.Errorf("%d application migrations are pending; run heya-metadata migrate up", len(status.Pending))
	}
	var riverInstalled bool
	if err := runtime.DB.QueryRow(ctx, `SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&riverInstalled); err != nil {
		return fmt.Errorf("inspect River schema: %w", err)
	}
	if !riverInstalled {
		return fmt.Errorf("River schema is missing; run heya-metadata migrate up")
	}
	return nil
}

func outputSmokeQueued(jobID int64) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued"})
	}
	ui.Success("Queued platform smoke job %d", jobID)
	return nil
}
