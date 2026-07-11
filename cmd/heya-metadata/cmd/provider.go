package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/sourcecollection"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newProviderCommand() *cobra.Command {
	command := &cobra.Command{Use: "provider", Short: "Collect provider source evidence", Args: cobra.NoArgs}
	command.AddCommand(newProviderCollectCommand())
	return command
}

func newProviderCollectCommand() *cobra.Command {
	var provider, identifierProvider, namespace, value, apiKey string
	var wait time.Duration
	command := &cobra.Command{
		Use:   "collect",
		Short: "Archive one known provider record through River and the shared cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if !registeredProvider(provider) {
				return fmt.Errorf("--provider must be one of %s", strings.Join(sourcecollection.RegisteredProviders(), ", "))
			}
			if identifierProvider == "" {
				identifierProvider = provider
			}
			if strings.TrimSpace(namespace) == "" || strings.TrimSpace(value) == "" {
				return fmt.Errorf("--namespace and --value are required")
			}
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
			credentialRef := ""
			if strings.TrimSpace(apiKey) != "" {
				credentialRef, err = providercredentials.Store(cmd.Context(), runtime.Redis, providercredentials.Credentials{
					APIKeys: map[string]string{provider: strings.TrimSpace(apiKey)},
				})
				if err != nil {
					return err
				}
			}
			client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
			if err != nil {
				return err
			}
			inserted, err := jobs.InsertSourceCollect(cmd.Context(), runtime, client, jobs.SourceCollectArgs{
				Provider: provider, IdentifierProvider: strings.ToLower(identifierProvider),
				Namespace: strings.ToLower(namespace), Value: strings.TrimSpace(value),
				CredentialRef: credentialRef, Reason: "cli",
			}, jobs.PriorityInteractive)
			if err != nil {
				return fmt.Errorf("enqueue source collection: %w", err)
			}
			if wait <= 0 {
				return outputProviderQueued(inserted.Job.ID, provider, namespace, value)
			}
			return waitForProvider(cmd, runtime, inserted.Job.ID, provider, namespace, value, wait)
		},
	}
	command.Flags().StringVar(&provider, "provider", "", "Collector name")
	command.Flags().StringVar(&identifierProvider, "id-provider", "", "Identifier provider (defaults to collector)")
	command.Flags().StringVar(&namespace, "namespace", "", "Identifier namespace")
	command.Flags().StringVar(&value, "value", "", "Identifier value")
	command.Flags().StringVar(&apiKey, "api-key", "", "Transient provider API key/token; never persisted")
	command.Flags().DurationVar(&wait, "wait", 60*time.Second, "Wait for completion (0 disables waiting)")
	_ = command.MarkFlagRequired("provider")
	_ = command.MarkFlagRequired("namespace")
	_ = command.MarkFlagRequired("value")
	return command
}

func waitForProvider(cmd *cobra.Command, runtime *platform.Runtime, jobID int64, provider, namespace, value string, wait time.Duration) error {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		var observationJSON []byte
		var reused, recorded int
		var failure *string
		err := runtime.DB.QueryRow(cmd.Context(), `
			SELECT state, observation_ids, reused_count, recorded_count, error
			FROM source_collection_runs WHERE river_job_id=$1`, jobID,
		).Scan(&state, &observationJSON, &reused, &recorded, &failure)
		if err == nil {
			if state == "completed" {
				var observationIDs []string
				_ = json.Unmarshal(observationJSON, &observationIDs)
				return outputProviderCompleted(jobID, provider, namespace, value, observationIDs, reused, recorded)
			}
			if state == "failed" && failure != nil {
				return fmt.Errorf("source collection failed: %s", *failure)
			}
		}
		var riverState rivertype.JobState
		if stateErr := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id=$1`, jobID).Scan(&riverState); stateErr == nil {
			if riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded {
				return fmt.Errorf("source collection job %d ended in state %s", jobID, riverState)
			}
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-deadline.C:
			return fmt.Errorf("timed out after %s waiting for source collection job %d", wait, jobID)
		case <-ticker.C:
		}
	}
}

func outputProviderQueued(jobID int64, provider, namespace, value string) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued", "provider": provider, "namespace": namespace, "value": value})
	}
	ui.Success("Queued %s %s:%s as job %d", provider, namespace, value, jobID)
	return nil
}

func outputProviderCompleted(jobID int64, provider, namespace, value string, observationIDs []string, reused, recorded int) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{
			"job_id": jobID, "state": "completed", "provider": provider,
			"namespace": namespace, "value": value, "observation_ids": observationIDs,
			"reused": reused, "recorded": recorded,
		})
	}
	ui.Success("Collected %s %s:%s", provider, namespace, value)
	ui.Info("Job", fmt.Sprintf("%d", jobID))
	ui.Info("Observations", strings.Join(observationIDs, ", "))
	ui.Info("Evidence", fmt.Sprintf("recorded=%d reused=%d", recorded, reused))
	return nil
}

func registeredProvider(provider string) bool {
	for _, candidate := range sourcecollection.RegisteredProviders() {
		if provider == candidate {
			return true
		}
	}
	return false
}
