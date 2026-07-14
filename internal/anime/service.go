package anime

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/anidb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/animelists"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/fanart"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/thexem"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
)

const tvdbAnimeNormalizerVersion = "tvdb-anime-series/v4"

var tmdbAnimeDefinition = episodic.Definition{Kind: "anime", Provider: "tmdb", Namespace: "tv", NormalizerVersion: "tmdb-anime/v1", MergeVersion: "anime-combiner/v9"}
var anidbDefinition = episodic.Definition{Kind: "anime", Provider: "anidb", Namespace: "anime", NormalizerVersion: "anidb-anime/v3", MergeVersion: "anime-combiner/v9"}
var tvmazeAnimeDefinition = episodic.Definition{Kind: "anime", Provider: "tvmaze", Namespace: "show", NormalizerVersion: "tvmaze-anime/v1", MergeVersion: "anime-combiner/v9"}

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }
func (s *Service) IngestTMDB(ctx context.Context, id string, jobID int64) (result episodic.Result, returnErr error) {
	return s.IngestTMDBWithCredentials(ctx, id, jobID, providercredentials.Credentials{})
}
func (s *Service) IngestTMDBWithCredentials(ctx context.Context, id string, jobID int64, credentials providercredentials.Credentials) (result episodic.Result, returnErr error) {
	if value, err := strconv.ParseInt(id, 10, 64); err != nil || value < 1 {
		return result, fmt.Errorf("invalid TMDB anime ID")
	}
	if err := episodic.StartRun(ctx, s.runtime, jobID, tmdbAnimeDefinition, id); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			episodic.FailRun(ctx, s.runtime, jobID, returnErr)
		}
	}()
	tmdbBase := tmdb.New(s.runtime.Config.Providers.TMDB)
	tmdbCache, err := providercache.New(s.runtime, tmdbAnimeDefinition.NormalizerVersion, tmdbBase.Capability().RawRetention, tmdbBase.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, tmdbCache, credentials.APIKey("tmdb")).CollectTV(ctx, providers.Identifier{Provider: "tmdb", Namespace: "tv", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("TMDB returned no anime detail")
	}
	if payloads[0].StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "tmdb", StatusCode: payloads[0].StatusCode}
	}
	if !tmdb.TVDetailIsAnimation(payloads[0].Body) {
		return result, &providers.StatusError{Provider: "tmdb", StatusCode: http.StatusNotFound}
	}
	record, err := tvshows.NormalizeTMDBTV(payloads, "anime")
	if err != nil {
		return result, err
	}
	record.NormalizerVersion = tmdbAnimeDefinition.NormalizerVersion
	records := []episodic.NormalizedRecord{record}
	tvdbID := episodicExternalID(record, "tvdb", "series")

	var mapping animelists.Entry
	mappingFound := false
	var mappingRecord *episodic.NormalizedRecord
	var mappingEntries []animelists.Entry
	if tvdbID != "" {
		mappingBase := animelists.New(s.runtime.Config.Providers.AnimeLists)
		mappingCache, cacheErr := providercache.New(s.runtime, "anime-lists-mapping/v2", mappingBase.Capability().RawRetention, mappingBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		mappingPayload, values, lookupErr := animelists.NewCached(s.runtime.Config.Providers.AnimeLists, mappingCache).LookupTVDBSeries(ctx, tvdbID)
		if lookupErr == nil && mappingPayload.StatusCode == http.StatusOK && len(values) > 0 {
			normalizedMapping, root, rootFound := normalizeTMDBAnimeListMapping(mappingPayload, id, values)
			mapping, mappingFound = root, rootFound
			mappingRecord = &normalizedMapping
			mappingEntries = values
		}
	}

	// AniDB is supplemental on the TMDB path. A rate limit, ban, or temporary
	// outage must not prevent an otherwise complete anime from materializing.
	if mappingFound && mapping.AniDBID > 0 {
		anidbBase := anidb.New(s.runtime.Config.Providers.AniDB)
		anidbCache, cacheErr := providercache.New(s.runtime, anidbDefinition.NormalizerVersion, anidbBase.Capability().RawRetention, anidbBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := anidb.NewCached(s.runtime.Config.Providers.AniDB, anidbCache).Collect(ctx, providers.Identifier{Provider: "anidb", Namespace: "anime", Value: strconv.Itoa(mapping.AniDBID)})
		if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := normalize(values[0]); normalizeErr == nil {
				supplemental.NormalizerVersion = anidbDefinition.NormalizerVersion
				records = append(records, supplemental)
			}
		}
	}

	if tvdbID != "" && (credentials.APIKey("tvdb") != "" || s.runtime.Config.Providers.TVDB.APIKey != "") {
		tvdbBase := tvdb.New(s.runtime.Config.Providers.TVDB)
		tvdbCache, cacheErr := providercache.New(s.runtime, tvdbAnimeNormalizerVersion, tvdbBase.Capability().RawRetention, tvdbBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := tvdb.NewCached(s.runtime.Config.Providers.TVDB, tvdbCache, credentials.APIKey("tvdb"), s.runtime.Redis).CollectSeries(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := tvshows.NormalizeTVDBSeries(values[0], "anime", nil, 0, values[1:]...); normalizeErr == nil {
				records = append(records, supplemental)
			}
		}
	}

	lookup := providers.Identifier{}
	if tvdbID != "" {
		lookup = providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID}
	} else if imdbID := episodicExternalID(record, "imdb", "title"); imdbID != "" {
		lookup = providers.Identifier{Provider: "imdb", Namespace: "title", Value: imdbID}
	}
	if lookup.Value != "" {
		mazeBase := tvmaze.New(s.runtime.Config.Providers.TVMaze)
		mazeCache, cacheErr := providercache.New(s.runtime, "tvmaze-anime/v1", mazeBase.Capability().RawRetention, mazeBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, mazeCache).Collect(ctx, lookup)
		if collectErr == nil && len(values) > 0 && values[len(values)-1].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := tvshows.NormalizeTVMaze(values[len(values)-1], "anime"); normalizeErr == nil {
				supplemental.NormalizerVersion = "tvmaze-anime/v1"
				records = append(records, supplemental)
			}
		}
	}

	if tvdbID != "" && (credentials.APIKey("fanart") != "" || s.runtime.Config.Providers.Fanart.APIKey != "") {
		fanartBase := fanart.New(s.runtime.Config.Providers.Fanart)
		fanartCache, cacheErr := providercache.New(s.runtime, fanart.TVNormalizerVersion, fanartBase.Capability().RawRetention, fanartBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := fanart.NewCached(s.runtime.Config.Providers.Fanart, fanartCache, credentials.APIKey("fanart")).Collect(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := fanart.NormalizeTV(values[0].Body, values[0].ObservationID, values[0].ObservedAt, "anime"); normalizeErr == nil {
				records = append(records, supplemental)
			}
		}
	}
	if tvdbID != "" {
		xemBase := thexem.New(s.runtime.Config.Providers.TheXEM)
		xemCache, cacheErr := providercache.New(s.runtime, thexem.NormalizerVersion, xemBase.Capability().RawRetention, xemBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		xemPayloads, collectErr := thexem.NewCached(s.runtime.Config.Providers.TheXEM, xemCache).Collect(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(xemPayloads) > 0 && xemPayloads[0].StatusCode == http.StatusOK {
			if supplemental, xemMapping, normalizeErr := thexem.NormalizeAnime(xemPayloads, tvdbID); normalizeErr == nil {
				if mappingRecord != nil {
					remapAnimeListSeasonEvidence(mappingRecord, mappingEntries, xemMapping.CanonicalAnimeSeason)
				}
				records = append(records, supplemental)
			}
		}
	}
	if mappingRecord != nil {
		records = append(records, *mappingRecord)
	}
	return episodic.PersistMany(ctx, s.runtime, tmdbAnimeDefinition, records, jobID)
}
func (s *Service) IngestTVMaze(ctx context.Context, id string, jobID int64) (result episodic.Result, returnErr error) {
	return s.IngestTVMazeWithCredentials(ctx, id, jobID, providercredentials.Credentials{})
}
func (s *Service) IngestTVMazeWithCredentials(ctx context.Context, id string, jobID int64, credentials providercredentials.Credentials) (result episodic.Result, returnErr error) {
	if value, err := strconv.ParseInt(id, 10, 64); err != nil || value < 1 {
		return result, fmt.Errorf("invalid TVMaze anime ID")
	}
	if err := episodic.StartRun(ctx, s.runtime, jobID, tvmazeAnimeDefinition, id); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			episodic.FailRun(ctx, s.runtime, jobID, returnErr)
		}
	}()
	mazeBase := tvmaze.New(s.runtime.Config.Providers.TVMaze)
	mazeCache, err := providercache.New(s.runtime, tvmazeAnimeDefinition.NormalizerVersion, mazeBase.Capability().RawRetention, mazeBase.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, mazeCache).Collect(ctx, providers.Identifier{Provider: "tvmaze", Namespace: "show", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("TVMaze returned no anime detail")
	}
	payload := payloads[len(payloads)-1]
	if payload.StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
	}
	record, err := tvshows.NormalizeTVMaze(payload, "anime")
	if err != nil {
		return result, err
	}
	record.NormalizerVersion = tvmazeAnimeDefinition.NormalizerVersion
	if credentials.APIKey("tmdb") != "" || s.runtime.Config.Providers.TMDB.Token != "" {
		lookupScheme, lookupValue := "", ""
		if value := episodicExternalID(record, "tvdb", "series"); value != "" {
			lookupScheme, lookupValue = "tvdb", value
		} else if value := episodicExternalID(record, "imdb", "title"); value != "" {
			lookupScheme, lookupValue = "imdb", value
		}
		if lookupValue != "" {
			tmdbBase := tmdb.New(s.runtime.Config.Providers.TMDB)
			tmdbCache, cacheErr := providercache.New(s.runtime, "tvmaze-anime-to-tmdb-root/v1", tmdbBase.Capability().RawRetention, tmdbBase.Capability().ResponseCache, jobID)
			if cacheErr != nil {
				return result, cacheErr
			}
			found, lookupErr := tmdb.NewCached(s.runtime.Config.Providers.TMDB, tmdbCache, credentials.APIKey("tmdb")).FindTVByExternal(ctx, lookupScheme, lookupValue)
			if lookupErr != nil {
				return result, lookupErr
			}
			if found.StatusCode == http.StatusOK {
				if tmdbID := tmdb.FirstTVResultID(found.Body); tmdbID != "" {
					return s.IngestTMDBWithCredentials(ctx, tmdbID, jobID, credentials)
				}
			} else if found.StatusCode != http.StatusNotFound {
				return result, &providers.StatusError{Provider: "tmdb", StatusCode: found.StatusCode}
			}
		}
	}
	records := []episodic.NormalizedRecord{record}
	tvdbID := episodicExternalID(record, "tvdb", "series")
	if tvdbID != "" && (credentials.APIKey("tvdb") != "" || s.runtime.Config.Providers.TVDB.APIKey != "") {
		tvdbBase := tvdb.New(s.runtime.Config.Providers.TVDB)
		tvdbCache, cacheErr := providercache.New(s.runtime, tvdbAnimeNormalizerVersion, tvdbBase.Capability().RawRetention, tvdbBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := tvdb.NewCached(s.runtime.Config.Providers.TVDB, tvdbCache, credentials.APIKey("tvdb"), s.runtime.Redis).CollectSeries(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := tvshows.NormalizeTVDBSeries(values[0], "anime", nil, 0, values[1:]...); normalizeErr == nil {
				records = append(records, supplemental)
			}
		}
	}
	if tvdbID != "" {
		mappingBase := animelists.New(s.runtime.Config.Providers.AnimeLists)
		mappingCache, cacheErr := providercache.New(s.runtime, "anime-lists-mapping/v2", mappingBase.Capability().RawRetention, mappingBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		mappingPayload, mapping, found, lookupErr := animelists.NewCached(s.runtime.Config.Providers.AnimeLists, mappingCache).LookupExternal(ctx, "tvdb", tvdbID)
		if lookupErr == nil && mappingPayload.StatusCode == http.StatusOK && found {
			mappingRecord := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "anime_lists", Namespace: "mapping", ProviderID: "tvmaze:" + id, PrimaryObservationID: mappingPayload.ObservationID, ObservedAt: mappingPayload.ObservedAt, NormalizerVersion: "anime-lists-mapping/v3/tvmaze/" + id}
			mappingRecord.ExternalIDs = appendExternalIDs(mappingRecord.ExternalIDs, animeListSeriesExternalIDs(mapping)...)
			if mapping.AniDBID > 0 {
				mappingRecord.ExternalIDs = appendExternalIDs(mappingRecord.ExternalIDs, episodic.ExternalID{Provider: "anidb", Namespace: "anime", Value: strconv.Itoa(mapping.AniDBID)})
			}
			if mapping.MALID > 0 {
				mappingRecord.ExternalIDs = appendExternalIDs(mappingRecord.ExternalIDs, episodic.ExternalID{Provider: "myanimelist", Namespace: "anime", Value: strconv.Itoa(mapping.MALID)})
			}
			if mapping.AniListID > 0 {
				mappingRecord.ExternalIDs = appendExternalIDs(mappingRecord.ExternalIDs, episodic.ExternalID{Provider: "anilist", Namespace: "anime", Value: strconv.Itoa(mapping.AniListID)})
			}
			records = append(records, mappingRecord)
			if mapping.AniDBID > 0 {
				anidbBase := anidb.New(s.runtime.Config.Providers.AniDB)
				anidbCache, cacheErr := providercache.New(s.runtime, anidbDefinition.NormalizerVersion, anidbBase.Capability().RawRetention, anidbBase.Capability().ResponseCache, jobID)
				if cacheErr != nil {
					return result, cacheErr
				}
				values, collectErr := anidb.NewCached(s.runtime.Config.Providers.AniDB, anidbCache).Collect(ctx, providers.Identifier{Provider: "anidb", Namespace: "anime", Value: strconv.Itoa(mapping.AniDBID)})
				if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
					if supplemental, normalizeErr := normalize(values[0]); normalizeErr == nil {
						supplemental.NormalizerVersion = anidbDefinition.NormalizerVersion
						records = append(records, supplemental)
					}
				}
			}
		}
	}
	if tvdbID != "" && (credentials.APIKey("fanart") != "" || s.runtime.Config.Providers.Fanart.APIKey != "") {
		fanartBase := fanart.New(s.runtime.Config.Providers.Fanart)
		fanartCache, cacheErr := providercache.New(s.runtime, fanart.TVNormalizerVersion, fanartBase.Capability().RawRetention, fanartBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		values, collectErr := fanart.NewCached(s.runtime.Config.Providers.Fanart, fanartCache, credentials.APIKey("fanart")).Collect(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := fanart.NormalizeTV(values[0].Body, values[0].ObservationID, values[0].ObservedAt, "anime"); normalizeErr == nil {
				records = append(records, supplemental)
			}
		}
	}
	return episodic.PersistMany(ctx, s.runtime, tvmazeAnimeDefinition, records, jobID)
}
func (s *Service) IngestAniDB(ctx context.Context, id string, jobID int64) (result episodic.Result, returnErr error) {
	return s.IngestAniDBWithCredentials(ctx, id, jobID, providercredentials.Credentials{})
}
func (s *Service) IngestAniDBWithCredentials(ctx context.Context, id string, jobID int64, credentials providercredentials.Credentials) (result episodic.Result, returnErr error) {
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n < 1 {
		return result, fmt.Errorf("invalid AniDB AID")
	}
	if err := episodic.StartRun(ctx, s.runtime, jobID, anidbDefinition, id); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			episodic.FailRun(ctx, s.runtime, jobID, returnErr)
		}
	}()
	// Resolve the static crosswalk before touching AniDB. When TMDB has the
	// series, an AniDB fallback candidate is only evidence and must be promoted
	// to the TMDB-rooted pipeline. This also lets ingestion proceed while AniDB
	// is rate-limited or temporarily banned.
	mappingBase := animelists.New(s.runtime.Config.Providers.AnimeLists)
	mappingCache, err := providercache.New(s.runtime, "anime-lists-mapping/v1", mappingBase.Capability().RawRetention, mappingBase.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	mappingClient := animelists.NewCached(s.runtime.Config.Providers.AnimeLists, mappingCache)
	mappingPayload, mapping, found, mappingErr := mappingClient.Lookup(ctx, id)
	if mappingErr == nil && found && (credentials.APIKey("tmdb") != "" || s.runtime.Config.Providers.TMDB.Token != "") {
		tmdbBase := tmdb.New(s.runtime.Config.Providers.TMDB)
		tmdbCache, cacheErr := providercache.New(s.runtime, "anidb-to-tmdb-root/v1", tmdbBase.Capability().RawRetention, tmdbBase.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		tmdbClient := tmdb.NewCached(s.runtime.Config.Providers.TMDB, tmdbCache, credentials.APIKey("tmdb"))
		tmdbID := ""
		if mapping.TMDBID.TV > 0 {
			candidateID := strconv.Itoa(mapping.TMDBID.TV)
			identity, identityErr := tmdbClient.TVExternalIDs(ctx, candidateID)
			if identityErr != nil {
				return result, identityErr
			}
			if identity.StatusCode == http.StatusOK {
				tmdbID = candidateID
			} else if identity.StatusCode != http.StatusNotFound {
				return result, &providers.StatusError{Provider: "tmdb", StatusCode: identity.StatusCode}
			}
		}
		if tmdbID == "" && mapping.TVDBID > 0 {
			identity, identityErr := tmdbClient.FindTVByExternal(ctx, "tvdb", strconv.Itoa(mapping.TVDBID))
			if identityErr != nil {
				return result, identityErr
			}
			if identity.StatusCode == http.StatusOK {
				tmdbID = tmdb.FirstTVResultID(identity.Body)
			} else if identity.StatusCode != http.StatusNotFound {
				return result, &providers.StatusError{Provider: "tmdb", StatusCode: identity.StatusCode}
			}
		}
		if tmdbID != "" {
			return s.IngestTMDBWithCredentials(ctx, tmdbID, jobID, credentials)
		}
	}
	base := anidb.New(s.runtime.Config.Providers.AniDB)
	resolver, err := providercache.New(s.runtime, anidbDefinition.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := anidb.NewCached(s.runtime.Config.Providers.AniDB, resolver).Collect(ctx, providers.Identifier{Provider: "anidb", Namespace: "anime", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("AniDB returned no anime detail")
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "anidb", StatusCode: payload.StatusCode}
	}
	record, err := normalize(payload)
	if err != nil {
		return result, err
	}
	record.NormalizerVersion = anidbDefinition.NormalizerVersion
	records := []episodic.NormalizedRecord{record}

	if mappingErr == nil && found {
		seriesRoot := mapping.IsTVDBSeriesRoot()
		rootAniDBID := id
		mappingSeriesIDs := animeListSeriesExternalIDs(mapping)
		if mapping.TVDBID > 0 && !seriesRoot {
			_, rootMapping, rootFound, rootErr := mappingClient.LookupExternal(ctx, "tvdb", strconv.Itoa(mapping.TVDBID))
			if rootErr == nil && rootFound && rootMapping.AniDBID > 0 {
				rootAniDBID = strconv.Itoa(rootMapping.AniDBID)
			}
		}
		if mapping.TVDBID > 0 {
			if seriesRoot {
				shared, evidenceErr := s.seriesExternalEvidence(ctx, strconv.Itoa(mapping.TVDBID))
				if evidenceErr != nil {
					return result, evidenceErr
				}
				record.ExternalIDs = appendExternalIDs(record.ExternalIDs, shared...)
			} else {
				var shared []episodic.ExternalID
				record.ExternalIDs, shared = splitAnimeSeriesExternalIDs(record.ExternalIDs)
				if evidenceErr := s.rememberSeriesExternalEvidence(ctx, strconv.Itoa(mapping.TVDBID), rootAniDBID, shared, record.PrimaryObservationID, record.ObservedAt); evidenceErr != nil {
					return result, evidenceErr
				}
				if evidenceErr := s.rememberSeriesExternalEvidence(ctx, strconv.Itoa(mapping.TVDBID), rootAniDBID, mappingSeriesIDs, mappingPayload.ObservationID, mappingPayload.ObservedAt); evidenceErr != nil {
					return result, evidenceErr
				}
			}
		}
		// record was copied into records before the mapping scope was known.
		// Keep the persisted AniDB normalization in sync with the filtered or
		// promoted series-level evidence above.
		records[0] = record
		mappingRecord := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "anime_lists", Namespace: "mapping", ProviderID: id, PrimaryObservationID: mappingPayload.ObservationID, ObservedAt: mappingPayload.ObservedAt, NormalizerVersion: "anime-lists-mapping/v2/anidb/" + id}
		if mapping.MALID > 0 {
			mappingRecord.ExternalIDs = append(mappingRecord.ExternalIDs, episodic.ExternalID{Provider: "myanimelist", Namespace: "anime", Value: strconv.Itoa(mapping.MALID)})
		}
		if mapping.AniListID > 0 {
			mappingRecord.ExternalIDs = append(mappingRecord.ExternalIDs, episodic.ExternalID{Provider: "anilist", Namespace: "anime", Value: strconv.Itoa(mapping.AniListID)})
		}
		if seriesRoot {
			mappingRecord.ExternalIDs = appendExternalIDs(mappingRecord.ExternalIDs, mappingSeriesIDs...)
		}
		records = append(records, mappingRecord)
		if mapping.TVDBID > 0 && mapping.Season.TVDB != nil && (credentials.APIKey("tvdb") != "" || s.runtime.Config.Providers.TVDB.APIKey != "") {
			tvdbBase := tvdb.New(s.runtime.Config.Providers.TVDB)
			tvdbCache, cacheErr := providercache.New(s.runtime, tvdbAnimeNormalizerVersion, tvdbBase.Capability().RawRetention, tvdbBase.Capability().ResponseCache, jobID)
			if cacheErr != nil {
				return result, cacheErr
			}
			payloads, collectErr := tvdb.NewCached(s.runtime.Config.Providers.TVDB, tvdbCache, credentials.APIKey("tvdb"), s.runtime.Redis).CollectSeries(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: strconv.Itoa(mapping.TVDBID)})
			if collectErr == nil && len(payloads) > 0 && payloads[0].StatusCode == http.StatusOK {
				if supplemental, normalizeErr := normalizeTVDBAnime(payloads[0], *mapping.Season.TVDB, mapping.EpisodeOffset.TVDB, payloads[1:]...); normalizeErr == nil {
					records = append(records, supplemental)
				}
			}
		}
		if mapping.TVDBID > 0 && mapping.Season.TVDB != nil && (credentials.APIKey("fanart") != "" || s.runtime.Config.Providers.Fanart.APIKey != "") {
			fanartBase := fanart.New(s.runtime.Config.Providers.Fanart)
			fanartCache, cacheErr := providercache.New(s.runtime, fanart.TVNormalizerVersion, fanartBase.Capability().RawRetention, fanartBase.Capability().ResponseCache, jobID)
			if cacheErr != nil {
				return result, cacheErr
			}
			payloads, collectErr := fanart.NewCached(s.runtime.Config.Providers.Fanart, fanartCache, credentials.APIKey("fanart")).Collect(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: strconv.Itoa(mapping.TVDBID)})
			if collectErr == nil && len(payloads) > 0 && payloads[0].StatusCode == http.StatusOK {
				if supplemental, normalizeErr := fanart.NormalizeTV(payloads[0].Body, payloads[0].ObservationID, payloads[0].ObservedAt, "anime"); normalizeErr == nil {
					supplemental.NormalizerVersion = fmt.Sprintf("%s/anime-season/%d", fanart.TVNormalizerVersion, *mapping.Season.TVDB)
					supplemental.Images = nil
					if !seriesRoot {
						supplemental.Namespace = "series_season"
						supplemental.ProviderID = fmt.Sprintf("%d:%d", mapping.TVDBID, *mapping.Season.TVDB)
						supplemental.ExternalIDs, _ = splitAnimeSeriesExternalIDs(supplemental.ExternalIDs)
					}
					mappedSeasons := make([]episodic.Season, 0, 1)
					for _, season := range supplemental.Seasons {
						if season.Number == *mapping.Season.TVDB {
							season.Number = 1
							mappedSeasons = append(mappedSeasons, season)
						}
					}
					supplemental.Seasons = mappedSeasons
					records = append(records, supplemental)
				}
			}
		}
	}
	return episodic.PersistMany(ctx, s.runtime, anidbDefinition, records, jobID)
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	return episodic.Resolve(ctx, s.runtime, "anime", provider, namespace, value)
}
func (s *Service) Detail(ctx context.Context, id string) (episodic.Document, bool, error) {
	return episodic.Detail(ctx, s.runtime, "anime", id)
}

type detail struct {
	XMLName      xml.Name
	ErrorMessage string `xml:",chardata"`
	ID           string `xml:"id,attr"`
	Type         string `xml:"type"`
	EpisodeCount int    `xml:"episodecount"`
	StartDate    string `xml:"startdate"`
	EndDate      string `xml:"enddate"`
	Description  string `xml:"description"`
	Picture      string `xml:"picture"`
	Titles       []struct {
		Language string `xml:"lang,attr"`
		Type     string `xml:"type,attr"`
		Value    string `xml:",chardata"`
	} `xml:"titles>title"`
	Creators []struct {
		Type string `xml:"type,attr"`
		Name string `xml:",chardata"`
	} `xml:"creators>name"`
	Tags []struct {
		Weight        int    `xml:"weight,attr"`
		LocalSpoiler  string `xml:"localspoiler,attr"`
		GlobalSpoiler string `xml:"globalspoiler,attr"`
		Name          string `xml:"name"`
	} `xml:"tags>tag"`
	Episodes []struct {
		ID     string `xml:"id,attr"`
		Number struct {
			Type  int    `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"epno"`
		Length  int    `xml:"length"`
		AirDate string `xml:"airdate"`
		Titles  []struct {
			Language string `xml:"lang,attr"`
			Value    string `xml:",chardata"`
		} `xml:"title"`
	} `xml:"episodes>episode"`
	Resources []struct {
		Type     int `xml:"type,attr"`
		External []struct {
			Identifier []string `xml:"identifier"`
		} `xml:"externalentity"`
	} `xml:"resources>resource"`
}

func normalize(payload providers.Payload) (episodic.NormalizedRecord, error) {
	var value detail
	if err := xml.Unmarshal(payload.Body, &value); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	if value.XMLName.Local == "error" {
		message := strings.ToLower(value.ErrorMessage)
		if strings.Contains(message, "not found") {
			// Cached observations created before application-level AniDB errors
			// were classified may still carry HTTP 200. Preserve correct
			// not-found semantics while those observations age out.
			return episodic.NormalizedRecord{}, &providers.StatusError{Provider: "anidb", StatusCode: http.StatusNotFound}
		}
		if strings.Contains(message, "banned") {
			return episodic.NormalizedRecord{}, &providers.StatusError{Provider: "anidb", StatusCode: http.StatusTooManyRequests}
		}
		return episodic.NormalizedRecord{}, fmt.Errorf("AniDB error response: %s", strings.TrimSpace(value.ErrorMessage))
	}
	if value.XMLName.Local != "anime" || value.ID == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid AniDB anime detail")
	}
	record := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "anidb", Namespace: "anime", ProviderID: value.ID, PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, Overview: strings.TrimSpace(value.Description), Format: normalizeType(value.Type), StartDate: value.StartDate, EndDate: value.EndDate, EpisodeCount: value.EpisodeCount, ExternalIDs: []episodic.ExternalID{{Provider: "anidb", Namespace: "anime", Value: value.ID}}}
	if record.Overview != "" {
		record.Overviews = []episodic.Text{{Value: record.Overview, Type: "overview"}}
	}
	for _, title := range value.Titles {
		kind := title.Type
		if kind == "main" {
			kind = "main"
		} else if title.Language == "ja" && title.Type == "official" {
			kind = "original"
		} else {
			kind = "alias"
		}
		record.Titles = append(record.Titles, episodic.Title{Value: strings.TrimSpace(title.Value), Language: title.Language, Type: kind})
	}
	for _, creator := range value.Creators {
		if strings.Contains(strings.ToLower(creator.Type), "animation work") {
			name := strings.TrimSpace(creator.Name)
			record.Studios = append(record.Studios, name)
			record.Organizations = append(record.Organizations, episodic.Organization{Name: name, Type: "studio"})
		}
	}
	for _, tag := range value.Tags {
		if tag.Weight >= 500 && tag.LocalSpoiler != "true" && tag.GlobalSpoiler != "true" {
			record.Genres = append(record.Genres, tag.Name)
		}
	}
	candidates := map[string][]episodic.ExternalID{}
	for _, resource := range value.Resources {
		for _, external := range resource.External {
			if len(external.Identifier) == 0 {
				continue
			}
			switch resource.Type {
			case 2:
				candidates["myanimelist:anime"] = append(candidates["myanimelist:anime"], episodic.ExternalID{Provider: "myanimelist", Namespace: "anime", Value: external.Identifier[0]})
			case 43:
				candidates["imdb:title"] = append(candidates["imdb:title"], episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: external.Identifier[0]})
			case 44:
				namespace := "tv"
				if len(external.Identifier) > 1 && external.Identifier[1] != "" {
					namespace = external.Identifier[1]
				}
				candidates["tmdb:"+namespace] = append(candidates["tmdb:"+namespace], episodic.ExternalID{Provider: "tmdb", Namespace: namespace, Value: external.Identifier[0]})
			}
		}
	}
	for _, ids := range candidates {
		unique := uniqueExternalIDs(ids)
		if len(unique) == 1 {
			record.ExternalIDs = append(record.ExternalIDs, unique[0])
		}
	}
	record.Genres = episodic.SortStrings(record.Genres)
	record.Studios = episodic.SortStrings(record.Studios)
	regularCount, specialCount := 0, 0
	for _, episode := range value.Episodes {
		titles := []episodic.Title{}
		for _, title := range episode.Titles {
			titles = append(titles, episodic.Title{Value: strings.TrimSpace(title.Value), Language: title.Language, Type: "main"})
		}
		scheme := "aired"
		episodeType := "regular"
		if episode.Number.Type == 2 {
			scheme = "special"
			episodeType = "special"
		} else if episode.Number.Type == 3 {
			scheme = "credit"
			episodeType = "credit"
		} else if episode.Number.Type == 4 {
			scheme = "trailer"
			episodeType = "trailer"
		} else if episode.Number.Type == 5 {
			scheme = "parody"
			episodeType = "parody"
		}
		number := animeEpisodeNumber(episode.Number.Value)
		season := 1
		isSpecial := scheme != "aired"
		if isSpecial {
			season = 0
			specialCount++
		} else {
			regularCount++
		}
		numbers := []episodic.EpisodeNumber{{Scheme: "aired", Season: season, Number: number, Provider: "anidb"}, {Scheme: "anidb", Season: season, Number: number, Provider: "anidb"}}
		if !isSpecial {
			numbers = append(numbers, episodic.EpisodeNumber{Scheme: "absolute", Number: number, Provider: "anidb"})
		} else {
			numbers = append(numbers, episodic.EpisodeNumber{Scheme: scheme, Season: 0, Number: number, Provider: "anidb"})
		}
		record.Episodes = append(record.Episodes, episodic.Episode{ProviderID: episode.ID, ExternalIDs: []episodic.ExternalID{{Provider: "anidb", Namespace: "episode", Value: episode.ID}}, Titles: titles, Numbers: numbers, IsSpecial: isSpecial, EpisodeType: episodeType, AirDate: episode.AirDate, RuntimeMinutes: episode.Length})
	}
	sort.SliceStable(record.Episodes, func(i, j int) bool {
		left, right := record.Episodes[i], record.Episodes[j]
		if left.IsSpecial != right.IsSpecial {
			return !left.IsSpecial
		}
		return left.Numbers[0].Number < right.Numbers[0].Number
	})
	if regularCount > 0 || value.EpisodeCount > 0 {
		record.Seasons = append(record.Seasons, episodic.Season{Number: 1, Name: "Season 1", Titles: []episodic.Title{{Value: "Season 1", Language: "en", Type: "display"}}, EpisodeCount: max(regularCount, value.EpisodeCount)})
	}
	if specialCount > 0 {
		record.Seasons = append(record.Seasons, episodic.Season{Number: 0, Name: "Specials", Titles: []episodic.Title{{Value: "Specials", Language: "en", Type: "display"}}, EpisodeCount: specialCount})
	}
	record.SeasonCount = len(record.Seasons)
	if value.Picture != "" {
		record.Images = append(record.Images, episodic.Image{Provider: "anidb", ProviderID: value.Picture, URL: "https://cdn-eu.anidb.net/images/main/" + value.Picture, Class: "poster"})
	}
	return record, nil
}

func uniqueExternalIDs(ids []episodic.ExternalID) []episodic.ExternalID {
	result := make([]episodic.ExternalID, 0, len(ids))
	for _, id := range ids {
		found := false
		for _, existing := range result {
			if existing.Value == id.Value {
				found = true
				break
			}
		}
		if !found {
			result = append(result, id)
		}
	}
	return result
}
func episodicExternalID(record episodic.NormalizedRecord, provider, namespace string) string {
	for _, id := range record.ExternalIDs {
		if id.Provider == provider && id.Namespace == namespace {
			return id.Value
		}
	}
	return ""
}
func normalizeType(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
func animeEpisodeNumber(value string) float64 {
	value = strings.TrimSpace(value)
	value = strings.TrimLeftFunc(value, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	number, _ := strconv.ParseFloat(value, 64)
	return number
}
func animeSchemeOrder(value string) int {
	switch value {
	case "aired":
		return 0
	case "special":
		return 1
	case "credit":
		return 2
	case "trailer":
		return 3
	case "parody":
		return 4
	}
	return 5
}
