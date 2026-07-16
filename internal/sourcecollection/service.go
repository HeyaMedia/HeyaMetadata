// Package sourcecollection archives provider evidence before canonical domain
// boundaries and merge rules exist for that source.
package sourcecollection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/ingest"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/anidb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/audiodb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/bandcamp"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/googlebooks"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/lastfm"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openlibrary"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openopus"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tidal"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/wikidata"
)

type Request struct {
	Provider    string
	Identifier  providers.Identifier
	JobID       int64
	Credentials providercredentials.Credentials
}

type Result struct {
	ObservationIDs []string
	ReusedCount    int
	RecordedCount  int
}

type factory struct {
	version    string
	capability providers.Capability
	build      func(providers.PayloadResolver) providers.Collector
}

func Collect(ctx context.Context, runtime *platform.Runtime, request Request) (result Result, returnErr error) {
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	spec, err := specification(runtime, provider, request.Credentials)
	if err != nil {
		return Result{}, err
	}
	if request.JobID > 0 {
		if _, err := runtime.DB.Exec(ctx, `
			INSERT INTO source_collection_runs (
				river_job_id, provider, identifier_provider, identifier_namespace, identifier_value, state
			) VALUES ($1, $2, $3, $4, $5, 'working')
			ON CONFLICT (river_job_id) DO UPDATE SET
				state='working', observation_ids='[]'::jsonb, reused_count=0,
				recorded_count=0, error=NULL, completed_at=NULL`,
			request.JobID, provider, request.Identifier.Provider, request.Identifier.Namespace, request.Identifier.Value,
		); err != nil {
			return Result{}, fmt.Errorf("start source collection run: %w", err)
		}
		defer func() {
			if returnErr != nil {
				observationJSON, _ := json.Marshal(result.ObservationIDs)
				_, _ = runtime.DB.Exec(context.WithoutCancel(ctx), `
					UPDATE source_collection_runs
					SET state='failed', observation_ids=$3, reused_count=$4, recorded_count=$5,
						error=$2, completed_at=now()
					WHERE river_job_id=$1`, request.JobID, returnErr.Error(), observationJSON, result.ReusedCount, result.RecordedCount)
			}
		}()
	}
	resolver, err := providercache.New(runtime, spec.version, spec.capability.RawRetention, spec.capability.ResponseCache, request.JobID)
	if err != nil {
		return Result{}, err
	}
	payloads, err := spec.build(resolver).Collect(ctx, request.Identifier)
	if err != nil {
		return Result{}, err
	}
	if len(payloads) == 0 {
		return Result{}, fmt.Errorf("%s collector returned no payloads", provider)
	}
	for _, payload := range payloads {
		if payload.ObservationID != "" {
			result.ObservationIDs = append(result.ObservationIDs, payload.ObservationID)
			if payload.FromCache {
				result.ReusedCount++
			} else {
				result.RecordedCount++
			}
		} else {
			recorded, recordErr := ingest.RecordObservation(ctx, runtime, payload, spec.version, spec.capability.RawRetention, spec.capability.ResponseCache, request.JobID)
			if recordErr != nil {
				return Result{}, recordErr
			}
			result.ObservationIDs = append(result.ObservationIDs, recorded.ID)
			result.RecordedCount++
		}
		if payload.StatusCode < http.StatusOK || payload.StatusCode >= http.StatusMultipleChoices {
			return result, &providers.StatusError{Provider: provider, StatusCode: payload.StatusCode}
		}
		if payload.ReuseDurationOverride != nil && *payload.ReuseDurationOverride == 0 {
			return result, fmt.Errorf("%s returned a non-reusable logical or malformed response", provider)
		}
	}
	if request.JobID > 0 {
		observationJSON, _ := json.Marshal(result.ObservationIDs)
		if _, err := runtime.DB.Exec(ctx, `
			UPDATE source_collection_runs
			SET state='completed', observation_ids=$2, reused_count=$3, recorded_count=$4,
				error=NULL, completed_at=now()
			WHERE river_job_id=$1`, request.JobID, observationJSON, result.ReusedCount, result.RecordedCount); err != nil {
			return Result{}, fmt.Errorf("complete source collection run: %w", err)
		}
	}
	return result, nil
}

func specification(runtime *platform.Runtime, provider string, credentials providercredentials.Credentials) (factory, error) {
	version := provider + "-source/v1"
	switch provider {
	case "musicbrainz":
		capability := musicbrainz.New(runtime.Config.Providers.MusicBrainz).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return musicbrainz.NewCached(runtime.Config.Providers.MusicBrainz, resolver)
		}}, nil
	case "apple":
		capability := apple.New(runtime.Config.Providers.Apple).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return apple.NewCached(runtime.Config.Providers.Apple, resolver, credentials.APIKey("apple"))
		}}, nil
	case "audiodb":
		capability := audiodb.New(runtime.Config.Providers.AudioDB).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return audiodb.NewCached(runtime.Config.Providers.AudioDB, resolver)
		}}, nil
	case "bandcamp":
		capability := bandcamp.New(runtime.Config.Providers.Bandcamp).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return bandcamp.NewCached(runtime.Config.Providers.Bandcamp, resolver)
		}}, nil
	case "deezer":
		capability := deezer.New(runtime.Config.Providers.Deezer).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return deezer.NewCached(runtime.Config.Providers.Deezer, resolver)
		}}, nil
	case "tidal":
		capability := tidal.New(runtime.Config.Providers.Tidal).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return tidal.NewCached(runtime.Config.Providers.Tidal, resolver)
		}}, nil
	case "discogs":
		capability := discogs.New(runtime.Config.Providers.Discogs).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return discogs.NewCached(runtime.Config.Providers.Discogs, resolver, credentials.APIKey("discogs"))
		}}, nil
	case "lastfm":
		capability := lastfm.New(runtime.Config.Providers.LastFM).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return lastfm.NewCached(runtime.Config.Providers.LastFM, resolver, credentials.APIKey("lastfm"))
		}}, nil
	case "anidb":
		capability := anidb.New(runtime.Config.Providers.AniDB).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return anidb.NewCached(runtime.Config.Providers.AniDB, resolver)
		}}, nil
	case "tvmaze":
		capability := tvmaze.New(runtime.Config.Providers.TVMaze).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return tvmaze.NewCached(runtime.Config.Providers.TVMaze, resolver)
		}}, nil
	case "wikidata":
		capability := wikidata.New(runtime.Config.Providers.Wikidata).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return wikidata.NewCached(runtime.Config.Providers.Wikidata, resolver)
		}}, nil
	case "openopus":
		capability := openopus.New(runtime.Config.Providers.OpenOpus).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return openopus.NewCached(runtime.Config.Providers.OpenOpus, resolver)
		}}, nil
	case "openlibrary":
		capability := openlibrary.New(runtime.Config.Providers.OpenLibrary).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return openlibrary.NewCached(runtime.Config.Providers.OpenLibrary, resolver)
		}}, nil
	case "googlebooks":
		capability := googlebooks.New(runtime.Config.Providers.GoogleBooks).Capability()
		return factory{version, capability, func(resolver providers.PayloadResolver) providers.Collector {
			return googlebooks.NewCached(runtime.Config.Providers.GoogleBooks, resolver, credentials.APIKey("googlebooks"))
		}}, nil
	default:
		return factory{}, fmt.Errorf("source collector %q is not registered", provider)
	}
}

func RegisteredProviders() []string {
	return []string{"anidb", "apple", "audiodb", "bandcamp", "deezer", "discogs", "googlebooks", "lastfm", "musicbrainz", "openlibrary", "openopus", "tidal", "tvmaze", "wikidata"}
}
