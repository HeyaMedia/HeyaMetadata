package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/people"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

func newPersonCommand() *cobra.Command {
	command := &cobra.Command{Use: "person", Short: "Manage canonical people", Args: cobra.NoArgs}
	command.AddCommand(newPersonEnrichCommand())
	command.AddCommand(newPersonReconcileCommand())
	return command
}

func newPersonReconcileCommand() *cobra.Command {
	command := &cobra.Command{Use: "reconcile", Short: "Review and decide canonical person identity candidates", Args: cobra.NoArgs}
	command.AddCommand(newPersonReconcileListCommand())
	command.AddCommand(newPersonReconcileAcceptCommand())
	command.AddCommand(newPersonReconcileRejectCommand())
	return command
}

func newPersonReconcileListCommand() *cobra.Command {
	var state string
	var limit int
	command := &cobra.Command{Use: "list", Short: "List reviewable person identity candidates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		items, err := people.NewService(runtime).ReconciliationCandidates(cmd.Context(), state, limit)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(items)
		}
		for _, item := range items {
			ui.Info(fmt.Sprintf("%.2f %s", item.Score, item.State), fmt.Sprintf("%s (%s) ↔ %s (%s)", item.LeftName, item.LeftPersonID, item.RightName, item.RightPersonID))
		}
		ui.Success("%d reconciliation candidates", len(items))
		return nil
	}}
	command.Flags().StringVar(&state, "state", "proposed", "Candidate state: proposed, accepted, rejected, or superseded")
	command.Flags().IntVar(&limit, "limit", 100, "Maximum candidates to return")
	return command
}

func newPersonReconcileAcceptCommand() *cobra.Command {
	var leftID, rightID, survivorID, actor, reason string
	command := &cobra.Command{Use: "accept", Short: "Merge an accepted person pair into an explicit surviving UUID", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		decision, err := people.NewService(runtime).AcceptReconciliation(cmd.Context(), leftID, rightID, survivorID, actor, reason)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(decision)
		}
		ui.Success("Person identities merged")
		ui.Info("Survivor", decision.SurvivorID)
		ui.Info("Retired", decision.RetiredID)
		ui.Info("Audit", decision.AuditLogID)
		return nil
	}}
	addPersonDecisionFlags(command, &leftID, &rightID, &actor, &reason)
	command.Flags().StringVar(&survivorID, "survivor", "", "Canonical person UUID to preserve")
	_ = command.MarkFlagRequired("survivor")
	return command
}

func newPersonReconcileRejectCommand() *cobra.Command {
	var leftID, rightID, actor, reason string
	command := &cobra.Command{Use: "reject", Short: "Reject a person identity candidate without changing either entity", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		decision, err := people.NewService(runtime).RejectReconciliation(cmd.Context(), leftID, rightID, actor, reason)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(decision)
		}
		ui.Success("Person reconciliation rejected")
		ui.Info("Audit", decision.AuditLogID)
		return nil
	}}
	addPersonDecisionFlags(command, &leftID, &rightID, &actor, &reason)
	return command
}

func addPersonDecisionFlags(command *cobra.Command, leftID, rightID, actor, reason *string) {
	defaultActor := os.Getenv("USER")
	if defaultActor == "" {
		defaultActor = "local-operator"
	}
	command.Flags().StringVar(leftID, "left", "", "First candidate person UUID")
	command.Flags().StringVar(rightID, "right", "", "Second candidate person UUID")
	command.Flags().StringVar(actor, "actor", defaultActor, "Operator recorded in the immutable audit entry")
	command.Flags().StringVar(reason, "reason", "", "Required moderation rationale")
	_ = command.MarkFlagRequired("left")
	_ = command.MarkFlagRequired("right")
	_ = command.MarkFlagRequired("reason")
}

func newPersonEnrichCommand() *cobra.Command {
	var entityID, tmdbID string
	command := &cobra.Command{Use: "enrich", Short: "Enrich an existing canonical person from an accepted TMDB claim", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		service := people.NewService(runtime)
		if err := service.EnrichTMDB(cmd.Context(), entityID, tmdbID, 0, providercredentials.Credentials{}); err != nil {
			return err
		}
		// A successful synchronous enrichment supersedes any queued background
		// copy for the same person (useful when diagnosing a worker locally).
		if client, clientErr := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false); clientErr == nil {
			rows, queryErr := runtime.DB.Query(cmd.Context(), `SELECT id FROM river_job WHERE kind=$1 AND args->>'entity_id'=$2 AND state IN('available','pending','retryable','running','scheduled')`, jobs.PersonEnrichKind, entityID)
			if queryErr == nil {
				var ids []int64
				for rows.Next() {
					var id int64
					if rows.Scan(&id) == nil {
						ids = append(ids, id)
					}
				}
				rows.Close()
				for _, id := range ids {
					_, _ = client.JobCancel(cmd.Context(), id)
				}
			}
		}
		document, err := service.Detail(cmd.Context(), entityID)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(document)
		}
		ui.Success("Person enriched")
		ui.Info("Entity", document.ID)
		ui.Info("Name", document.Display.Title)
		ui.Info("Credits", strconv.Itoa(document.Data.CreditTotal))
		return nil
	}}
	command.Flags().StringVar(&entityID, "id", "", "Canonical person UUID")
	command.Flags().StringVar(&tmdbID, "tmdb", "", "Accepted TMDB person ID")
	_ = command.MarkFlagRequired("id")
	_ = command.MarkFlagRequired("tmdb")
	return command
}
