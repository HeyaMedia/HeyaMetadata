package cmd

import (
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
	"strconv"
	"strings"
	"time"
)

func newDiscoverCommand() *cobra.Command {
	command := &cobra.Command{Use: "discover", Short: "Discover and rank unknown upstream entities", Args: cobra.NoArgs}
	command.AddCommand(newDiscoverArtistCommand())
	command.AddCommand(newDiscoverMovieCommand())
	command.AddCommand(newDiscoverReleaseGroupCommand())
	command.AddCommand(newDiscoverRecordingCommand())
	command.AddCommand(newDiscoverMusicalWorkCommand())
	command.AddCommand(newDiscoverTVCommand())
	command.AddCommand(newDiscoverAnimeCommand())
	command.AddCommand(newDiscoverBookCommand())
	return command
}

func newDiscoverMusicalWorkCommand() *cobra.Command {
	var query, catalogue string
	var composers, composerIDs []string
	var limit int
	var wait time.Duration
	command := &cobra.Command{Use: "musical-work", Short: "Discover Open Opus composed works with structured hints", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runDiscovery(cmd, discovery.Request{Kind: discovery.KindMusicalWork, Query: query, Limit: limit, Hints: discovery.Hints{Composers: composers, ComposerIDs: composerIDs, Catalogue: catalogue}}, wait, providercredentials.Credentials{})
	}}
	command.Flags().StringSliceVar(&composers, "composer", nil, "Known composer name; repeat or comma-separate")
	command.Flags().StringSliceVar(&composerIDs, "composer-openopus", nil, "Known Open Opus composer ID; repeat or comma-separate")
	command.Flags().StringVar(&catalogue, "catalogue", "", "Known catalogue reference, e.g. op. 67 or BWV 1007")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverBookCommand() *cobra.Command {
	var query string
	var authors, isbns []string
	var year, limit int
	var wait time.Duration
	command := &cobra.Command{Use: "book", Short: "Discover Open Library book works", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runDiscovery(cmd, discovery.Request{Kind: discovery.KindBookWork, Query: query, Limit: limit, Hints: discovery.Hints{Authors: authors, ISBNs: isbns, Year: year}}, wait, providercredentials.Credentials{})
	}}
	command.Flags().StringSliceVar(&authors, "author", nil, "Known author; repeat or comma-separate")
	command.Flags().StringSliceVar(&isbns, "isbn", nil, "Known ISBN; repeat or comma-separate")
	command.Flags().IntVar(&year, "year", 0, "First publication year hint")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverRecordingCommand() *cobra.Command {
	var query string
	var artists, artistIDs, isrcs, releases []string
	var durationMS int64
	var limit int
	var wait time.Duration
	command := &cobra.Command{Use: "recording", Short: "Discover MusicBrainz recording candidates with structured hints", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		request := discovery.Request{Kind: discovery.KindRecording, Query: query, Limit: limit, Hints: discovery.Hints{Artists: artists, ArtistIDs: artistIDs, ISRCs: isrcs, DurationMS: durationMS}}
		for _, value := range releases {
			hint, err := parseReleaseHint(value)
			if err != nil {
				return err
			}
			request.Hints.Releases = append(request.Hints.Releases, hint)
		}
		return runDiscovery(cmd, request, wait, providercredentials.Credentials{})
	}}
	command.Flags().StringSliceVar(&artists, "artist", nil, "Credited artist name; repeat or comma-separate")
	command.Flags().StringSliceVar(&artistIDs, "artist-mbid", nil, "Credited MusicBrainz artist ID; repeat or comma-separate")
	command.Flags().StringSliceVar(&isrcs, "isrc", nil, "Known ISRC; repeat or comma-separate")
	command.Flags().StringSliceVar(&releases, "release", nil, "Known release as title or title:year; repeatable")
	command.Flags().Int64Var(&durationMS, "duration-ms", 0, "Known recording duration in milliseconds")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverTVCommand() *cobra.Command {
	var query, country, language, network, status, apiKey string
	var year, limit int
	var episodes []string
	var wait time.Duration
	command := &cobra.Command{Use: "tv", Short: "Discover TMDB-rooted conventional TV candidates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		hints := discovery.Hints{Country: country, Language: language, Network: network, Status: status, Year: year}
		for _, title := range episodes {
			hints.Episodes = append(hints.Episodes, discovery.EpisodeHint{Title: title})
		}
		credentials := providercredentials.Credentials{}
		if strings.TrimSpace(apiKey) != "" {
			credentials.APIKeys = map[string]string{"tmdb": apiKey}
		}
		return runDiscovery(cmd, discovery.Request{Kind: discovery.KindTVShow, Query: query, Limit: limit, Hints: hints}, wait, credentials)
	}}
	command.Flags().IntVar(&year, "year", 0, "Premiere year hint")
	command.Flags().StringVar(&country, "country", "", "ISO country hint")
	command.Flags().StringVar(&language, "language", "", "ISO original-language hint")
	command.Flags().StringVar(&network, "network", "", "Broadcast network or streaming service hint")
	command.Flags().StringVar(&status, "status", "", "Show status hint")
	command.Flags().StringSliceVar(&episodes, "episode", nil, "Known episode title; repeat or comma-separate")
	command.Flags().StringVar(&apiKey, "api-key", "", "Request-scoped TMDB API key")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverAnimeCommand() *cobra.Command {
	var query, format, apiKey string
	var year, count, limit int
	var episodes []string
	var wait time.Duration
	command := &cobra.Command{Use: "anime", Short: "Discover TMDB-rooted Anime candidates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		hints := discovery.Hints{Year: year, Type: format, EpisodeCount: count}
		for _, title := range episodes {
			hints.Episodes = append(hints.Episodes, discovery.EpisodeHint{Title: title})
		}
		credentials := providercredentials.Credentials{}
		if strings.TrimSpace(apiKey) != "" {
			credentials.APIKeys = map[string]string{"tmdb": apiKey}
		}
		return runDiscovery(cmd, discovery.Request{Kind: discovery.KindAnime, Query: query, Limit: limit, Hints: hints}, wait, credentials)
	}}
	command.Flags().IntVar(&year, "year", 0, "Start year hint")
	command.Flags().StringVar(&format, "type", "", "Anime format hint, e.g. tv_series, movie, or ova")
	command.Flags().IntVar(&count, "episode-count", 0, "Known episode count")
	command.Flags().StringSliceVar(&episodes, "episode", nil, "Known episode title; repeat or comma-separate")
	command.Flags().StringVar(&apiKey, "api-key", "", "Request-scoped TMDB API key")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
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
		return runDiscovery(cmd, request, wait, providercredentials.Credentials{})
	}}
	command.Flags().StringVar(&country, "country", "", "ISO country hint, e.g. JP")
	command.Flags().StringVar(&artistType, "type", "", "Artist type hint, e.g. person or group")
	command.Flags().StringVar(&beginDate, "begin-date", "", "Birth/founding date hint")
	command.Flags().StringVar(&endDate, "end-date", "", "Death/dissolution date hint")
	command.Flags().StringSliceVar(&aliases, "alias", nil, "Known alias; repeat or comma-separate")
	command.Flags().StringSliceVar(&releases, "release", nil, "Known release as title or title:year; repeatable")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverMovieCommand() *cobra.Command {
	var query, country, language, originalTitle, date, apiKey string
	var aliases []string
	var year, limit int
	var wait time.Duration
	command := &cobra.Command{Use: "movie", Short: "Discover TMDB movie candidates with structured hints", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		request := discovery.Request{Kind: discovery.KindMovie, Query: query, Limit: limit, Hints: discovery.Hints{Country: country, Language: language, Year: year, Date: date, OriginalTitle: originalTitle, Aliases: aliases}}
		credentials := providercredentials.Credentials{}
		if strings.TrimSpace(apiKey) != "" {
			credentials.APIKeys = map[string]string{"tmdb": apiKey}
		}
		return runDiscovery(cmd, request, wait, credentials)
	}}
	command.Flags().IntVar(&year, "year", 0, "Release year hint")
	command.Flags().StringVar(&date, "date", "", "Release date hint (YYYY-MM-DD)")
	command.Flags().StringVar(&country, "country", "", "ISO origin-country hint")
	command.Flags().StringVar(&language, "language", "", "ISO original-language hint")
	command.Flags().StringVar(&originalTitle, "original-title", "", "Known original title")
	command.Flags().StringSliceVar(&aliases, "alias", nil, "Known alternate title; repeat or comma-separate")
	command.Flags().StringVar(&apiKey, "api-key", "", "Request-scoped TMDB API key")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func newDiscoverReleaseGroupCommand() *cobra.Command {
	var query, releaseType, date string
	var artists, artistIDs, tracks []string
	var year, limit int
	var wait time.Duration
	command := &cobra.Command{Use: "release-group", Short: "Discover MusicBrainz release-group candidates with structured hints", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		request := discovery.Request{Kind: discovery.KindReleaseGroup, Query: query, Limit: limit, Hints: discovery.Hints{Year: year, Date: date, Type: releaseType, Artists: artists, ArtistIDs: artistIDs, Tracks: tracks}}
		return runDiscovery(cmd, request, wait, providercredentials.Credentials{})
	}}
	command.Flags().IntVar(&year, "year", 0, "First-release year hint")
	command.Flags().StringVar(&date, "date", "", "First-release date hint")
	command.Flags().StringVar(&releaseType, "type", "", "Primary or secondary type hint, e.g. album or soundtrack")
	command.Flags().StringSliceVar(&artists, "artist", nil, "Credited artist name; repeat or comma-separate")
	command.Flags().StringSliceVar(&artistIDs, "artist-mbid", nil, "Credited MusicBrainz artist ID; repeat or comma-separate")
	command.Flags().StringSliceVar(&tracks, "track", nil, "Known track title; repeat or comma-separate")
	addDiscoveryCommonFlags(command, &query, &limit, &wait)
	return command
}

func addDiscoveryCommonFlags(command *cobra.Command, query *string, limit *int, wait *time.Duration) {
	command.Flags().StringVar(query, "query", "", "Name or title to discover")
	command.Flags().IntVar(limit, "limit", 10, "Maximum candidates (1-25)")
	command.Flags().DurationVar(wait, "wait", 30*time.Second, "Maximum wait for provider discovery")
	_ = command.MarkFlagRequired("query")
}

func runDiscovery(cmd *cobra.Command, request discovery.Request, wait time.Duration, credentials providercredentials.Credentials) error {
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
		credentialRef, err := providercredentials.Store(cmd.Context(), runtime.Redis, credentials)
		if err != nil {
			return err
		}
		inserted, err := jobs.InsertDiscovery(cmd.Context(), runtime, client, run, credentialRef)
		if err != nil {
			_ = providercredentials.Delete(cmd.Context(), runtime.Redis, credentialRef)
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
		label := candidate.Display.Name
		if label == "" {
			label = candidate.Display.Title
		}
		detail := candidate.Display.Disambiguation
		if detail == "" && candidate.Display.Year > 0 {
			detail = strconv.Itoa(candidate.Display.Year)
		}
		fmt.Printf("%d. %.3f %-8s %s — %s%s\n", candidate.Rank, candidate.Confidence, candidate.Match, label, detail, existing)
	}
	return nil
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
