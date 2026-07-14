package anime

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/animelists"
	"github.com/jackc/pgx/v5"
)

func animeListSeriesExternalIDs(entry animelists.Entry) []episodic.ExternalID {
	result := []episodic.ExternalID{}
	if entry.TVDBID > 0 {
		result = append(result, episodic.ExternalID{Provider: "tvdb", Namespace: "series", Value: strconv.Itoa(entry.TVDBID)})
	}
	if entry.TMDBID.TV > 0 {
		result = append(result, episodic.ExternalID{Provider: "tmdb", Namespace: "tv", Value: strconv.Itoa(entry.TMDBID.TV)})
	}
	for _, imdbID := range entry.IMDbIDs {
		if value := strings.TrimSpace(imdbID); value != "" {
			result = appendExternalIDs(result, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: value})
		}
	}
	return result
}

func animeEntryExternalIDs(entry animelists.Entry) []episodic.ExternalID {
	result := []episodic.ExternalID{}
	if entry.AniDBID > 0 {
		result = append(result, episodic.ExternalID{Provider: "anidb", Namespace: "anime", Value: strconv.Itoa(entry.AniDBID)})
	}
	if entry.MALID > 0 {
		result = append(result, episodic.ExternalID{Provider: "myanimelist", Namespace: "anime", Value: strconv.Itoa(entry.MALID)})
	}
	if entry.AniListID > 0 {
		result = append(result, episodic.ExternalID{Provider: "anilist", Namespace: "anime", Value: strconv.Itoa(entry.AniListID)})
	}
	return result
}

func normalizeTMDBAnimeListMapping(payload providers.Payload, tmdbID string, values []animelists.Entry) (episodic.NormalizedRecord, animelists.Entry, bool) {
	record := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "anime_lists", Namespace: "mapping", ProviderID: "tmdb:" + tmdbID, PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, NormalizerVersion: "anime-lists-mapping/v4/tmdb/" + tmdbID}
	seasonIndexes := map[int]int{}
	var root animelists.Entry
	rootFound := false
	for _, value := range values {
		ids := animeEntryExternalIDs(value)
		if !rootFound && value.IsTVDBSeriesRoot() {
			root, rootFound = value, true
			record.ExternalIDs = appendExternalIDs(record.ExternalIDs, animeListSeriesExternalIDs(value)...)
			record.ExternalIDs = appendExternalIDs(record.ExternalIDs, ids...)
		}
		if value.Season.TVDB == nil || *value.Season.TVDB < 1 {
			continue
		}
		seasonNumber := *value.Season.TVDB
		index, exists := seasonIndexes[seasonNumber]
		if !exists {
			index = len(record.Seasons)
			seasonIndexes[seasonNumber] = index
			record.Seasons = append(record.Seasons, episodic.Season{Number: seasonNumber})
		}
		record.Seasons[index].ExternalIDs = appendExternalIDs(record.Seasons[index].ExternalIDs, ids...)
	}
	return record, root, rootFound
}

// remapAnimeListSeasonEvidence moves cour-scoped identities from Anime Lists'
// TVDB season/offset address onto the canonical season partition supplied by
// TheXEM. This is necessary for series such as 86 EIGHTY-SIX, where TVDB and
// TMDB flatten two AniDB seasons into one aired season.
func remapAnimeListSeasonEvidence(record *episodic.NormalizedRecord, values []animelists.Entry, canonicalSeason func(tvdbSeason, firstEpisode int) (int, bool)) {
	if record == nil || canonicalSeason == nil {
		return
	}
	seasonIndexes := map[int]int{}
	seasons := []episodic.Season{}
	for _, value := range values {
		if value.Season.TVDB == nil || *value.Season.TVDB < 1 {
			continue
		}
		seasonNumber := *value.Season.TVDB
		if mapped, ok := canonicalSeason(seasonNumber, value.EpisodeOffset.TVDB+1); ok && mapped >= 0 {
			seasonNumber = mapped
		}
		index, exists := seasonIndexes[seasonNumber]
		if !exists {
			index = len(seasons)
			seasonIndexes[seasonNumber] = index
			seasons = append(seasons, episodic.Season{Number: seasonNumber})
		}
		seasons[index].ExternalIDs = appendExternalIDs(seasons[index].ExternalIDs, animeEntryExternalIDs(value)...)
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
	record.Seasons = seasons
}

func splitAnimeSeriesExternalIDs(values []episodic.ExternalID) (entityIDs, seriesIDs []episodic.ExternalID) {
	for _, value := range values {
		value.Provider = strings.ToLower(strings.TrimSpace(value.Provider))
		value.Namespace = strings.ToLower(strings.TrimSpace(value.Namespace))
		value.Value = strings.TrimSpace(value.Value)
		if value.Value == "" {
			continue
		}
		if isAnimeSeriesExternalID(value) {
			seriesIDs = appendExternalIDs(seriesIDs, value)
		} else {
			entityIDs = appendExternalIDs(entityIDs, value)
		}
	}
	return entityIDs, seriesIDs
}

func isAnimeSeriesExternalID(value episodic.ExternalID) bool {
	return (value.Provider == "imdb" && value.Namespace == "title") ||
		(value.Provider == "tmdb" && value.Namespace == "tv") ||
		(value.Provider == "tvdb" && value.Namespace == "series")
}

func appendExternalIDs(values []episodic.ExternalID, additions ...episodic.ExternalID) []episodic.ExternalID {
	for _, addition := range additions {
		duplicate := false
		for _, existing := range values {
			if strings.EqualFold(existing.Provider, addition.Provider) &&
				strings.EqualFold(existing.Namespace, addition.Namespace) &&
				strings.EqualFold(existing.Value, addition.Value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			values = append(values, addition)
		}
	}
	return values
}

func (s *Service) seriesExternalEvidence(ctx context.Context, tvdbSeriesID string) ([]episodic.ExternalID, error) {
	rows, err := s.runtime.DB.Query(ctx, `
		SELECT provider,namespace,normalized_value
		FROM episodic_series_external_evidence
		WHERE entity_kind='anime'
		  AND anchor_provider='tvdb'
		  AND anchor_namespace='series'
		  AND anchor_value=$1
		ORDER BY provider,namespace,normalized_value`, strings.TrimSpace(tvdbSeriesID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []episodic.ExternalID{}
	for rows.Next() {
		var value episodic.ExternalID
		if err := rows.Scan(&value.Provider, &value.Namespace, &value.Value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Service) rememberSeriesExternalEvidence(ctx context.Context, tvdbSeriesID, rootAniDBID string, values []episodic.ExternalID, observationID string, observedAt time.Time) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, value := range values {
		if !isAnimeSeriesExternalID(value) {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO episodic_series_external_evidence(
				entity_kind,anchor_provider,anchor_namespace,anchor_value,
				provider,namespace,normalized_value,source_observation_id,
				first_observed_at,last_observed_at
			) VALUES('anime','tvdb','series',$1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$6)
			ON CONFLICT(entity_kind,anchor_provider,anchor_namespace,anchor_value,provider,namespace,normalized_value)
			DO UPDATE SET source_observation_id=EXCLUDED.source_observation_id,
			              last_observed_at=EXCLUDED.last_observed_at`,
			strings.TrimSpace(tvdbSeriesID), value.Provider, value.Namespace,
			strings.ToLower(strings.TrimSpace(value.Value)), observationID, observedAt)
		if err != nil {
			return err
		}
	}

	var rootEntityID string
	err = tx.QueryRow(ctx, `
		SELECT entity_id::text
		FROM external_id_claims
		WHERE entity_kind='anime' AND provider='anidb' AND namespace='anime'
		  AND normalized_value=$1 AND state='accepted'`, strings.TrimSpace(rootAniDBID)).Scan(&rootEntityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}

	for _, value := range values {
		if !isAnimeSeriesExternalID(value) {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_id_claims(
				entity_id,entity_kind,provider,namespace,normalized_value,state,
				confidence,source_observation_id,first_observed_at,last_observed_at
			) VALUES($1,'anime',$2,$3,$4,'accepted',1,NULLIF($5,'')::uuid,$6,$6)
			ON CONFLICT(entity_kind,provider,namespace,normalized_value)
			DO UPDATE SET entity_id=EXCLUDED.entity_id,state='accepted',confidence=1,
			              source_observation_id=EXCLUDED.source_observation_id,
			              last_observed_at=EXCLUDED.last_observed_at`,
			rootEntityID, value.Provider, value.Namespace,
			strings.ToLower(strings.TrimSpace(value.Value)), observationID, observedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
