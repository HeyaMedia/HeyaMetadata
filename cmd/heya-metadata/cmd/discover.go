package cmd

import (
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
	"strconv"
	"strings"
	"time"
)

func newDiscoverCommand() *cobra.Command {
	command := &cobra.Command{Use: "discover", Short: "Discover and rank unknown upstream entities", Args: cobra.NoArgs}
	command.AddCommand(newDiscoverArtistCommand())
	return command
}
func newDiscoverArtistCommand() *cobra.Command {
	var query, country, artistType, beginDate, endDate string
	var aliases, releases []string
	var limit int
	var wait time.Duration
	command := &cobra.Command{Use: "artist", Short: "Discover MusicBrainz artist candidates with structured hints", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		request := discovery.Request{Kind: discovery.KindArtist, Query: query, Limit: limit, Hints: discovery.Hints{Country: country, Type: artistType, BeginDate: beginDate, EndDate: endDate, Aliases: aliases}}
		for _, value := range releases {
			hint, err := parseReleaseHint(value)
			if err != nil {
				return err
			}
			request.Hints.Releases = append(request.Hints.Releases, hint)
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
		run, err := discovery.EnsureRun(cmd.Context(), runtime, request)
		if err != nil {
			return err
		}
		client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
		if err != nil {
			return err
		}
		if run.State == "queued" {
			inserted, err := jobs.InsertDiscovery(cmd.Context(), runtime, client, run)
			if err != nil {
				return err
			}
			run.RiverJobID = inserted.Job.ID
		}
		if run.State != "completed" {
			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for run.State != "completed" && run.State != "failed" {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-deadline.C:
					return fmt.Errorf("timed out waiting for discovery %s", run.ID)
				case <-ticker.C:
					run, err = discovery.GetRun(cmd.Context(), runtime, run.ID)
					if err != nil {
						return err
					}
				}
			}
		}
		if run.State == "failed" {
			return fmt.Errorf("discovery failed: %s", run.Error)
		}
		if ui.JSONMode {
			return ui.OutputJSON(run)
		}
		ui.Success("Discovery %s: %s", run.ID, run.Result.Recommendation)
		for _, candidate := range run.Result.Candidates {
			existing := ""
			if candidate.ExistingEntityID != "" {
				existing = " [canonical " + candidate.ExistingEntityID + "]"
			}
			fmt.Printf("%d. %.3f %-8s %s — %s%s\n", candidate.Rank, candidate.Confidence, candidate.Match, candidate.Display.Name, candidate.Display.Disambiguation, existing)
		}
		return nil
	}}
	command.Flags().StringVar(&query, "query", "", "Artist name or alias")
	command.Flags().StringVar(&country, "country", "", "ISO country hint, e.g. JP")
	command.Flags().StringVar(&artistType, "type", "", "Artist type hint, e.g. person or group")
	command.Flags().StringVar(&beginDate, "begin-date", "", "Birth/founding date hint")
	command.Flags().StringVar(&endDate, "end-date", "", "Death/dissolution date hint")
	command.Flags().StringSliceVar(&aliases, "alias", nil, "Known alias; repeat or comma-separate")
	command.Flags().StringSliceVar(&releases, "release", nil, "Known release as title or title:year; repeatable")
	command.Flags().IntVar(&limit, "limit", 10, "Maximum candidates (1-25)")
	command.Flags().DurationVar(&wait, "wait", 30*time.Second, "Maximum wait for provider discovery")
	_ = command.MarkFlagRequired("query")
	return command
}
func parseReleaseHint(value string) (discovery.ReleaseHint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return discovery.ReleaseHint{}, fmt.Errorf("release hint must not be empty")
	}
	at := strings.LastIndex(value, ":")
	if at < 0 {
		return discovery.ReleaseHint{Title: value}, nil
	}
	year, err := strconv.Atoi(value[at+1:])
	if err != nil || year < 1000 || year > 3000 {
		return discovery.ReleaseHint{Title: value}, nil
	}
	return discovery.ReleaseHint{Title: strings.TrimSpace(value[:at]), Year: year}, nil
}
