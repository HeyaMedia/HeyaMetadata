// Package artists orchestrates canonical artist evidence collection and projection.
package artists

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/HeyaMedia/HeyaMetadata/internal/ingest"
	"github.com/HeyaMedia/HeyaMetadata/internal/mixer"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/fanart"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/lastfm"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/wikidata"
	"github.com/jackc/pgx/v5"
)

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

type Result struct {
	EntityID          string
	NormalizedID      string
	ProjectionVersion int64
	Detail            artistdomain.DetailDocument
}
type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) IngestMusicBrainz(ctx context.Context, mbid string, riverJobID int64, credentials providercredentials.Credentials) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if riverJobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO artist_ingestion_runs (river_job_id,musicbrainz_id,state) VALUES ($1,$2,'working') ON CONFLICT (river_job_id) DO UPDATE SET state='working',error=NULL,completed_at=NULL`, riverJobID, mbid); err != nil {
			return Result{}, fmt.Errorf("start artist ingestion run: %w", err)
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE artist_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, riverJobID, returnErr.Error())
			}
		}()
	}
	mbCapability := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz).Capability()
	mbResolver, err := providercache.New(s.runtime, artistdomain.MusicBrainzNormalizerVersion, mbCapability.RawRetention, mbCapability.ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	mbCollector := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, mbResolver)
	payloads, err := mbCollector.Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: mbid})
	if err != nil {
		return Result{}, err
	}
	observations, err := s.recordPayloads(ctx, payloads, artistdomain.MusicBrainzNormalizerVersion, mbCapability, riverJobID)
	if err != nil {
		return Result{}, err
	}
	if len(observations) == 0 {
		return Result{}, fmt.Errorf("MusicBrainz collector returned no observations")
	}
	primary := observations[0]
	if primary.Payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: primary.Payload.StatusCode}
	}
	spine, err := musicbrainz.NormalizeArtist(primary.Payload.Body, primary.ID, primary.Payload.ObservedAt)
	if err != nil {
		return Result{}, err
	}
	expectedLastFMNames := artistNames(spine)
	known := []providers.Identifier{{Provider: "musicbrainz", Namespace: "artist", Value: mbid}}
	for _, candidate := range spine.IdentityCandidates {
		known = append(known, providers.Identifier{Provider: candidate.Provider, Namespace: candidate.Namespace, Value: candidate.NormalizedValue})
	}
	collectors := []providers.Collector{}
	for _, build := range []func() (providers.Collector, error){
		func() (providers.Collector, error) {
			c := apple.New(s.runtime.Config.Providers.Apple)
			r, e := providercache.New(s.runtime, artistdomain.AppleNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return apple.NewCached(s.runtime.Config.Providers.Apple, r, credentials.APIKey("apple")), e
		},
		func() (providers.Collector, error) {
			c := deezer.New(s.runtime.Config.Providers.Deezer)
			r, e := providercache.New(s.runtime, artistdomain.DeezerNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return deezer.NewCached(s.runtime.Config.Providers.Deezer, r), e
		},
		func() (providers.Collector, error) {
			c := discogs.New(s.runtime.Config.Providers.Discogs)
			r, e := providercache.New(s.runtime, artistdomain.DiscogsNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return discogs.NewCached(s.runtime.Config.Providers.Discogs, r, credentials.APIKey("discogs")), e
		},
		func() (providers.Collector, error) {
			c := lastfm.New(s.runtime.Config.Providers.LastFM)
			r, e := providercache.New(s.runtime, artistdomain.LastFMNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return lastfm.NewCached(s.runtime.Config.Providers.LastFM, r, credentials.APIKey("lastfm")), e
		},
		func() (providers.Collector, error) {
			c := fanart.New(s.runtime.Config.Providers.Fanart)
			r, e := providercache.New(s.runtime, artistdomain.FanartNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return fanart.NewCached(s.runtime.Config.Providers.Fanart, r, credentials.APIKey("fanart")), e
		},
		func() (providers.Collector, error) {
			c := wikidata.New(s.runtime.Config.Providers.Wikidata)
			r, e := providercache.New(s.runtime, artistdomain.WikidataNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, riverJobID)
			return wikidata.NewCached(s.runtime.Config.Providers.Wikidata, r), e
		},
	} {
		collector, buildErr := build()
		if buildErr != nil {
			return Result{}, buildErr
		}
		collectors = append(collectors, collector)
	}
	desired := []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeRatings, providers.ScopeArtwork, providers.ScopeRecommendations}
	plan := mixer.New(collectors...).BuildAllAvailable(known, desired, nil)
	records := []artistdomain.NormalizedRecordV1{spine}
	failures := map[string]error{}
	for _, step := range plan.Steps {
		provider := step.Collector.Capability().Provider
		payloads, collectErr := step.Collector.Collect(ctx, step.Identifier)
		if collectErr != nil {
			failures[provider] = collectErr
			continue
		}
		version := artistNormalizerVersion(provider)
		recorded, recordErr := s.recordPayloads(ctx, payloads, version, step.Collector.Capability(), riverJobID)
		if recordErr != nil || len(recorded) == 0 {
			if recordErr == nil {
				recordErr = fmt.Errorf("collector returned no observations")
			}
			failures[provider] = recordErr
			continue
		}
		if recorded[0].Payload.StatusCode != http.StatusOK {
			failures[provider] = &providers.StatusError{Provider: provider, StatusCode: recorded[0].Payload.StatusCode}
			continue
		}
		var normalized artistdomain.NormalizedRecordV1
		switch provider {
		case "apple":
			normalized, recordErr = apple.NormalizeArtist(recorded[0].Payload.Body, step.Identifier.Value, recorded[0].ID, recorded[0].Payload.ObservedAt)
		case "deezer":
			normalized, recordErr = deezer.NormalizeArtist(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
		case "discogs":
			normalized, recordErr = discogs.NormalizeArtist(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
		case "lastfm":
			normalized, recordErr = lastfm.NormalizeArtist(recorded[0].Payload.Body, mbid, expectedLastFMNames, recorded[0].ID, recorded[0].Payload.ObservedAt)
			lastFMNameLookup := ""
			if recordErr != nil {
				fallbackResolver, fallbackErr := providercache.New(s.runtime, artistdomain.LastFMNormalizerVersion, step.Collector.Capability().RawRetention, step.Collector.Capability().ResponseCache, riverJobID)
				if fallbackErr != nil {
					recordErr = fallbackErr
				} else {
					lastFMClient := lastfm.NewCached(s.runtime.Config.Providers.LastFM, fallbackResolver, credentials.APIKey("lastfm"))
					normalized, lastFMNameLookup, recordErr = s.collectLastFMArtistByName(ctx, lastFMClient, step.Collector.Capability(), mbid, expectedLastFMNames, riverJobID)
				}
			}
			if recordErr == nil {
				topCapability := lastfm.New(s.runtime.Config.Providers.LastFM).Capability()
				topResolver, topErr := providercache.New(s.runtime, artistdomain.LastFMTopTracksVersion, topCapability.RawRetention, topCapability.ResponseCache, riverJobID)
				if topErr == nil {
					topClient := lastfm.NewCached(s.runtime.Config.Providers.LastFM, topResolver, credentials.APIKey("lastfm"))
					var snapshot lastfm.TopTracksSnapshot
					var topRecorded ingest.RecordedObservation
					snapshot, topRecorded, topErr = s.collectLastFMTopTracks(ctx, topClient, topCapability, mbid, expectedLastFMNames, lastFMNameLookup, riverJobID)
					if topErr == nil {
						normalized.TopTracks = snapshot.Tracks
						normalized.TopTracksObserved = true
						normalized.TopTracksTotal = snapshot.Total
						normalized.TopTracksObservationID = topRecorded.ID
						normalized.TopTracksObservedAt = topRecorded.Payload.ObservedAt
						normalized.ProviderRecord.SupportingObservationIDs = append(normalized.ProviderRecord.SupportingObservationIDs, topRecorded.ID)
						if snapshot.NameScoped || lastFMNameLookup != "" {
							normalized.Warnings = append(normalized.Warnings, "Last.fm top tracks retained as a name-scoped aggregate")
						}
					}
				}
				if topErr != nil {
					normalized.PartialFailure = true
					normalized.Warnings = append(normalized.Warnings, "lastfm.top_tracks: "+topErr.Error())
					slog.Warn("artist top tracks provider failed", "provider", "lastfm", "mbid", mbid, "error", topErr)
				}
			}
		case "fanart":
			normalized, recordErr = fanart.NormalizeMusicArtist(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
		case "wikidata":
			normalized, recordErr = wikidata.NormalizeArtist(recorded[0].Payload.Body, step.Identifier.Value, recorded[0].ID, recorded[0].Payload.ObservedAt)
		}
		if recordErr != nil {
			failures[provider] = recordErr
			continue
		}
		records = append(records, normalized)
	}
	if len(failures) > 0 {
		spine.PartialFailure = true
		keys := make([]string, 0, len(failures))
		for key := range failures {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			spine.Warnings = append(spine.Warnings, key+": "+failures[key].Error())
			slog.Warn("supplemental artist provider failed", "provider", key, "mbid", mbid, "error", failures[key])
		}
		records[0] = spine
	}
	normalizedIDs := make([]string, 0, len(records))
	for _, record := range records {
		id, recordErr := s.recordNormalized(ctx, record)
		if recordErr != nil {
			return Result{}, recordErr
		}
		normalizedIDs = append(normalizedIDs, id)
	}
	result, err = s.merge(ctx, normalizedIDs, records, riverJobID)
	if err != nil {
		return Result{}, err
	}
	if err := s.cache(ctx, result); err != nil {
		return Result{}, err
	}
	if err := changelog.Sequence(ctx, s.runtime, 100); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Service) collectLastFMArtistByName(ctx context.Context, client *lastfm.Client, capability providers.Capability, mbid string, expectedNames []string, riverJobID int64) (artistdomain.NormalizedRecordV1, string, error) {
	var lastErr error
	for _, name := range lastFMNameCandidates(expectedNames) {
		payload, err := client.ArtistInfoByName(ctx, name)
		if err != nil {
			lastErr = err
			continue
		}
		recorded, err := s.recordPayloads(ctx, []providers.Payload{payload}, artistdomain.LastFMNormalizerVersion, capability, riverJobID)
		if err != nil || len(recorded) == 0 {
			if err == nil {
				err = fmt.Errorf("Last.fm name lookup returned no observation")
			}
			lastErr = err
			continue
		}
		if recorded[0].Payload.StatusCode != http.StatusOK {
			lastErr = &providers.StatusError{Provider: "lastfm", StatusCode: recorded[0].Payload.StatusCode}
			continue
		}
		normalized, err := lastfm.NormalizeArtist(recorded[0].Payload.Body, mbid, expectedNames, recorded[0].ID, recorded[0].Payload.ObservedAt)
		if err != nil {
			lastErr = err
			continue
		}
		normalized.Warnings = append(normalized.Warnings, "Last.fm artist retained from an exact canonical-name lookup")
		return normalized, name, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Last.fm artist has no usable canonical-name fallback")
	}
	return artistdomain.NormalizedRecordV1{}, "", lastErr
}

func (s *Service) collectLastFMTopTracks(ctx context.Context, client *lastfm.Client, capability providers.Capability, mbid string, expectedNames []string, profileNameLookup string, riverJobID int64) (lastfm.TopTracksSnapshot, ingest.RecordedObservation, error) {
	lookups := make([]string, 0, len(expectedNames)+1)
	if profileNameLookup == "" {
		lookups = append(lookups, "") // Prefer the durable MBID when it works.
	} else {
		lookups = append(lookups, profileNameLookup)
	}
	lookups = append(lookups, lastFMNameCandidates(expectedNames)...)
	seen := map[string]bool{}
	var lastErr error
	for _, name := range lookups {
		key := strings.ToLower(strings.Join(strings.Fields(name), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		var payload providers.Payload
		var err error
		if name == "" {
			payload, err = client.ArtistTopTracks(ctx, mbid, 100, 1)
		} else {
			payload, err = client.ArtistTopTracksByName(ctx, name, 100, 1)
		}
		if err != nil {
			lastErr = err
			continue
		}
		recorded, err := s.recordPayloads(ctx, []providers.Payload{payload}, artistdomain.LastFMTopTracksVersion, capability, riverJobID)
		if err != nil || len(recorded) == 0 {
			if err == nil {
				err = fmt.Errorf("Last.fm top tracks returned no observation")
			}
			lastErr = err
			continue
		}
		if recorded[0].Payload.StatusCode != http.StatusOK {
			lastErr = &providers.StatusError{Provider: "lastfm", StatusCode: recorded[0].Payload.StatusCode}
			continue
		}
		snapshot, err := lastfm.NormalizeArtistTopTracks(recorded[0].Payload.Body, mbid, expectedNames)
		if err != nil {
			lastErr = err
			continue
		}
		if name != "" {
			snapshot.NameScoped = true
			for index := range snapshot.Tracks {
				snapshot.Tracks[index].RecordingMBID = ""
			}
		}
		return snapshot, recorded[0], nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Last.fm top tracks have no usable lookup")
	}
	return lastfm.TopTracksSnapshot{}, ingest.RecordedObservation{}, lastErr
}

func lastFMNameCandidates(names []string) []string {
	result := make([]string, 0, min(len(names), 8))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(strings.Join(strings.Fields(name), " "))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, name)
		if len(result) == 8 {
			break
		}
	}
	return result
}

func artistNormalizerVersion(provider string) string {
	switch provider {
	case "apple":
		return artistdomain.AppleNormalizerVersion
	case "deezer":
		return artistdomain.DeezerNormalizerVersion
	case "discogs":
		return artistdomain.DiscogsNormalizerVersion
	case "lastfm":
		return artistdomain.LastFMNormalizerVersion
	case "fanart":
		return artistdomain.FanartNormalizerVersion
	case "wikidata":
		return artistdomain.WikidataNormalizerVersion
	}
	return provider + "-artist/v1"
}

func (s *Service) recordPayloads(ctx context.Context, payloads []providers.Payload, version string, capability providers.Capability, jobID int64) ([]ingest.RecordedObservation, error) {
	result := make([]ingest.RecordedObservation, 0, len(payloads))
	for _, payload := range payloads {
		if payload.ObservationID != "" {
			result = append(result, ingest.RecordedObservation{ID: payload.ObservationID, Checksum: payload.BlobChecksum, Payload: payload})
			continue
		}
		recorded, err := ingest.RecordObservation(ctx, s.runtime, payload, version, capability.RawRetention, capability.ResponseCache, jobID)
		if err != nil {
			return nil, err
		}
		result = append(result, recorded)
	}
	return result, nil
}
func (s *Service) recordNormalized(ctx context.Context, record artistdomain.NormalizedRecordV1) (string, error) {
	document, _ := json.Marshal(record)
	supporting, _ := json.Marshal(record.ProviderRecord.SupportingObservationIDs)
	warnings, _ := json.Marshal(record.Warnings)
	var id string
	err := s.runtime.DB.QueryRow(ctx, `INSERT INTO normalized_records (entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,supporting_observation_ids,normalizer_version,schema_version,document,warnings,partial_failure,observed_at) VALUES ('artist',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (primary_observation_id,normalizer_version,schema_version) DO UPDATE SET document=EXCLUDED.document,warnings=EXCLUDED.warnings,partial_failure=EXCLUDED.partial_failure RETURNING id`, record.ProviderRecord.Provider, record.ProviderRecord.Namespace, record.ProviderRecord.Value, record.ProviderRecord.PrimaryObservationID, supporting, record.ProviderRecord.NormalizerVersion, record.ProviderRecord.SchemaVersion, document, warnings, record.PartialFailure, record.ProviderRecord.ObservedAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record normalized %s artist: %w", record.ProviderRecord.Provider, err)
	}
	return id, nil
}

func (s *Service) merge(ctx context.Context, normalizedIDs []string, successful []artistdomain.NormalizedRecordV1, jobID int64) (Result, error) {
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	entityIDs := map[string]bool{}
	var allCandidates []artistdomain.IdentityCandidate
	spineMusicBrainzID := successful[0].ProviderRecord.Value
	for _, record := range successful {
		for _, candidate := range record.IdentityCandidates {
			if candidate.Confidence < 1 {
				continue
			}
			if candidate.Provider == "musicbrainz" && candidate.Namespace == "artist" && candidate.NormalizedValue != spineMusicBrainzID {
				continue
			}
			allCandidates = append(allCandidates, candidate)
			var id string
			err := tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='artist' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, candidate.Provider, candidate.Namespace, candidate.NormalizedValue).Scan(&id)
			if err == nil {
				entityIDs[id] = true
			} else if err != pgx.ErrNoRows {
				return Result{}, err
			}
		}
	}
	if len(entityIDs) > 1 {
		claims, _ := json.Marshal(allCandidates)
		_, _ = tx.Exec(ctx, `INSERT INTO external_id_conflicts (entity_kind,claims,normalized_record_id) VALUES ('artist',$1,$2)`, claims, normalizedIDs[0])
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("artist claims resolve to multiple canonical artists")
	}
	entityID := ""
	created := false
	for id := range entityIDs {
		entityID = id
	}
	if entityID == "" {
		created = true
		base := artistSlug(preferredName(successful[0]))
		for suffix := 0; ; suffix++ {
			slug := base
			if suffix > 0 {
				slug = fmt.Sprintf("%s-%d", base, suffix+1)
			}
			err := tx.QueryRow(ctx, `INSERT INTO entities (kind,slug) VALUES ('artist',$1) ON CONFLICT DO NOTHING RETURNING id`, slug).Scan(&entityID)
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO entity_slugs (entity_id,kind,slug) VALUES ($1,'artist',$2)`, entityID, slug)
				if err != nil {
					return Result{}, err
				}
				break
			}
			if err != pgx.ErrNoRows {
				return Result{}, err
			}
		}
	}
	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1 FOR UPDATE`, entityID).Scan(&slug); err != nil {
		return Result{}, err
	}
	for _, record := range successful {
		for _, candidate := range record.IdentityCandidates {
			if candidate.Confidence < 1 {
				continue
			}
			if candidate.Provider == "musicbrainz" && candidate.Namespace == "artist" && candidate.NormalizedValue != spineMusicBrainzID {
				continue
			}
			tag, err := tx.Exec(ctx, `INSERT INTO external_id_claims (entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at) VALUES ($1,'artist',$2,$3,$4,'accepted',$5,$6,$7,$7) ON CONFLICT (entity_kind,provider,namespace,normalized_value) DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, candidate.Provider, candidate.Namespace, candidate.NormalizedValue, candidate.Confidence, record.ProviderRecord.PrimaryObservationID, record.ProviderRecord.ObservedAt)
			if err != nil {
				return Result{}, err
			}
			if tag.RowsAffected() == 0 {
				return Result{}, fmt.Errorf("external ID %s.%s:%s belongs to another artist", candidate.Provider, candidate.Namespace, candidate.NormalizedValue)
			}
		}
	}
	for _, id := range normalizedIDs {
		if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, id); err != nil {
			return Result{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (provider,provider_namespace,provider_record_id) id,document FROM normalized_records WHERE entity_id=$1 AND entity_kind='artist' ORDER BY provider,provider_namespace,provider_record_id,observed_at DESC,created_at DESC`, entityID)
	if err != nil {
		return Result{}, err
	}
	var records []artistdomain.RecordInput
	for rows.Next() {
		var id string
		var document []byte
		if err := rows.Scan(&id, &document); err != nil {
			rows.Close()
			return Result{}, err
		}
		var record artistdomain.NormalizedRecordV1
		if err := json.Unmarshal(document, &record); err != nil {
			rows.Close()
			return Result{}, err
		}
		records = append(records, artistdomain.RecordInput{ID: id, Record: record})
	}
	rows.Close()
	// Last.fm now serves the same placeholder star for artist images. Retire any
	// candidates created by older normalizers whenever the artist is refreshed.
	if _, err := tx.Exec(ctx, `DELETE FROM image_candidates WHERE entity_id=$1 AND provider='lastfm'`, entityID); err != nil {
		return Result{}, fmt.Errorf("retire Last.fm artist images: %w", err)
	}
	imageIDs := map[string]string{}
	for _, input := range records {
		if input.Record.ProviderRecord.Provider == "lastfm" {
			continue
		}
		for _, image := range input.Record.Images {
			if image.SourceURL == "" {
				continue
			}
			providerImageID := image.ProviderImageID
			if providerImageID == "" {
				digest := sha256.Sum256([]byte(image.SourceURL))
				providerImageID = hex.EncodeToString(digest[:8])
			}
			var imageID string
			err := tx.QueryRow(ctx, `INSERT INTO image_candidates (entity_id,provider,provider_image_id,class,source_url,language,width,height,provider_score,source_observation_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (entity_id,provider,provider_image_id,class) DO UPDATE SET source_url=EXCLUDED.source_url,language=EXCLUDED.language,width=EXCLUDED.width,height=EXCLUDED.height,provider_score=EXCLUDED.provider_score,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, input.Record.ProviderRecord.Provider, providerImageID, image.Class, image.SourceURL, image.Language, image.Width, image.Height, image.ProviderScore, input.Record.ProviderRecord.PrimaryObservationID).Scan(&imageID)
			if err != nil {
				return Result{}, err
			}
			keyImage := image
			keyImage.ProviderImageID = providerImageID
			imageIDs[artistdomain.ImageKey(input.Record.ProviderRecord.Provider, keyImage)] = imageID
			imageIDs[artistdomain.ImageKey(input.Record.ProviderRecord.Provider, image)] = imageID
		}
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	projection := artistdomain.Combine(entityID, slug, version, records, imageIDs, now)
	topTracksChanged, err := replaceTopTracks(ctx, tx, entityID, successful, version)
	if err != nil {
		return Result{}, err
	}
	detailJSON, _ := json.Marshal(projection.Detail)
	summaryJSON, _ := json.Marshal(projection.Summary)
	provenanceJSON, _ := json.Marshal(projection.Detail.Provenance)
	sourceJSON, _ := json.Marshal(records)
	digest := sha256.Sum256(append([]byte(artistdomain.MergeVersion+":"), sourceJSON...))
	fingerprint := hex.EncodeToString(digest[:])
	if _, err := tx.Exec(ctx, `INSERT INTO canonical_artists (entity_id,merge_version,source_fingerprint,document) VALUES ($1,$2,$3,$4) ON CONFLICT (entity_id) DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, artistdomain.MergeVersion, fingerprint, detailJSON); err != nil {
		return Result{}, err
	}
	for _, document := range []struct {
		kind string
		body []byte
	}{{"detail", detailJSON}, {"summary", summaryJSON}} {
		if _, err := tx.Exec(ctx, `INSERT INTO api_documents (entity_id,document_kind,schema_version,projection_version,document,fresh_until) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (entity_id,document_kind) DO UPDATE SET schema_version=EXCLUDED.schema_version,projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now() WHERE api_documents.projection_version<=EXCLUDED.projection_version`, entityID, document.kind, artistdomain.ProjectionSchemaVersion, version, document.body, projection.Detail.Freshness.FreshUntil); err != nil {
			return Result{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO api_document_provenance (entity_id,document_kind,projection_version,document) VALUES ($1,'detail',$2,$3) ON CONFLICT (entity_id,document_kind) DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document`, entityID, version, provenanceJSON); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_entities (entity_id,kind,slug,display_title,status,genres,countries,languages,summary,projection_version) VALUES ($1,'artist',$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (entity_id) DO UPDATE SET slug=EXCLUDED.slug,display_title=EXCLUDED.display_title,status=EXCLUDED.status,genres=EXCLUDED.genres,countries=EXCLUDED.countries,languages=EXCLUDED.languages,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, projection.Detail.Display.Name, projection.Detail.Data.Classification.ArtistType, projection.Summary.Genres, areaCodes(projection.Detail.Data.Areas), nameLanguages(projection.SearchNames), summaryJSON, version); err != nil {
		return Result{}, err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID)
	for _, name := range projection.SearchNames {
		if strings.TrimSpace(name.Value) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO search_names (entity_id,value,normalized_value,locale,name_type,source_quality) VALUES ($1,$2,lower(unaccent($2)),$3,$4,$5) ON CONFLICT DO NOTHING`, entityID, name.Value, name.Language, name.Type, nameQuality(name)); err != nil {
			return Result{}, err
		}
	}
	for _, record := range successful {
		_, err := tx.Exec(ctx, `INSERT INTO provider_refresh_states (entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at) VALUES ($1,$2,now(),now(),$3,$4,$5) ON CONFLICT (entity_id,provider) DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,failure_class=NULL,failure_message=NULL,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at`, entityID, record.ProviderRecord.Provider, record.ProviderRecord.PrimaryObservationID, nullableJob(jobID), projection.Detail.Freshness.FreshUntil)
		if err != nil {
			return Result{}, err
		}
	}
	changeType := "updated"
	if created {
		changeType = "created"
	}
	changedScopes := []string{"identity", "detail", "search", "provenance"}
	if topTracksChanged {
		changedScopes = append(changedScopes, "top_tracks")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO change_outbox (entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id) VALUES ($1,'artist',$2,$3,$4,$5,$6,$7)`, entityID, slug, changeType, changedScopes, version, successful[0].ProviderRecord.PrimaryObservationID, nullableJob(jobID)); err != nil {
		return Result{}, err
	}
	if jobID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE artist_ingestion_runs SET entity_id=$2,state='completed',completed_at=now(),error=NULL WHERE river_job_id=$1`, jobID, entityID); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{EntityID: entityID, NormalizedID: normalizedIDs[0], ProjectionVersion: version, Detail: projection.Detail}, nil
}

func replaceTopTracks(ctx context.Context, tx pgx.Tx, entityID string, records []artistdomain.NormalizedRecordV1, projectionVersion int64) (bool, error) {
	changed := false
	for _, record := range records {
		if !record.TopTracksObserved {
			continue
		}
		changed = true
		provider := record.ProviderRecord.Provider
		observationID := record.TopTracksObservationID
		if observationID == "" {
			observationID = record.ProviderRecord.PrimaryObservationID
		}
		observedAt := record.TopTracksObservedAt
		if observedAt.IsZero() {
			observedAt = record.ProviderRecord.ObservedAt
		}
		if _, err := tx.Exec(ctx, `DELETE FROM artist_top_tracks WHERE artist_entity_id=$1 AND provider=$2`, entityID, provider); err != nil {
			return false, fmt.Errorf("replace %s artist top tracks: %w", provider, err)
		}
		for _, track := range record.TopTracks {
			if _, err := tx.Exec(ctx, `INSERT INTO artist_top_tracks(artist_entity_id,provider,rank,title,provider_track_id,recording_mbid,playcount,listeners,url,source_observation_id,observed_at,projection_version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid,$11,$12)`, entityID, provider, track.Rank, track.Title, track.ProviderTrackID, track.RecordingMBID, track.Playcount, track.Listeners, track.URL, observationID, observedAt, projectionVersion); err != nil {
				return false, fmt.Errorf("persist %s artist top track rank %d: %w", provider, track.Rank, err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO artist_top_track_snapshots(artist_entity_id,provider,item_count,reported_total,source_observation_id,observed_at,projection_version)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7)ON CONFLICT(artist_entity_id,provider)DO UPDATE SET item_count=EXCLUDED.item_count,reported_total=EXCLUDED.reported_total,source_observation_id=EXCLUDED.source_observation_id,observed_at=EXCLUDED.observed_at,projection_version=EXCLUDED.projection_version`, entityID, provider, len(record.TopTracks), record.TopTracksTotal, observationID, observedAt, projectionVersion); err != nil {
			return false, fmt.Errorf("persist %s artist top-track snapshot: %w", provider, err)
		}
	}
	return changed, nil
}

func (s *Service) cache(ctx context.Context, result Result) error {
	body, err := json.Marshal(result.Detail)
	if err != nil {
		return err
	}
	ttl := time.Until(result.Detail.Freshness.FreshUntil)
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := s.runtime.Redis.Set(ctx, "heya:metadata:v1:api:entity:"+result.EntityID+":detail", body, ttl).Err(); err != nil {
		return err
	}
	return s.runtime.Redis.Publish(ctx, "heya:metadata:v1:cache-invalidations", result.EntityID).Err()
}
func preferredName(record artistdomain.NormalizedRecordV1) string {
	for _, name := range record.Names {
		if name.Primary && name.Value != "" {
			return name.Value
		}
	}
	for _, name := range record.Names {
		if name.Value != "" {
			return name.Value
		}
	}
	return "artist"
}

func artistNames(record artistdomain.NormalizedRecordV1) []string {
	result := make([]string, 0, len(record.Names))
	for _, name := range record.Names {
		if value := strings.TrimSpace(name.Value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
func artistSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "artist"
	}
	return value
}
func areaCodes(areas []artistdomain.Area) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, area := range areas {
		for _, code := range area.ISOCodes {
			if !seen[code] {
				seen[code] = true
				out = append(out, code)
			}
		}
	}
	return out
}
func nameLanguages(names []artistdomain.Name) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, name := range names {
		if name.Language != "" && !seen[name.Language] {
			seen[name.Language] = true
			out = append(out, name.Language)
		}
	}
	return out
}
func nameQuality(name artistdomain.Name) int {
	if name.Primary {
		return 100
	}
	if name.Type == "display" || name.Type == "label" {
		return 80
	}
	return 50
}
func nullableJob(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
