// Package releasegroups orchestrates canonical work-level music releases.
package releasegroups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/ingest"
	"github.com/HeyaMedia/HeyaMetadata/internal/mixer"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/coverartarchive"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/fanart"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/wikidata"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/canonicalrefs"
)

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)
var ErrNotFound = fmt.Errorf("release group not found")

type Result struct {
	EntityID, NormalizedID string
	ProjectionVersion      int64
	Detail                 rgdomain.DetailDocument
}
type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) IngestMusicBrainz(ctx context.Context, mbid string, jobID int64, credentials providercredentials.Credentials) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if jobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO release_group_ingestion_runs (river_job_id,musicbrainz_id,state) VALUES ($1,$2,'working') ON CONFLICT (river_job_id) DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, mbid); err != nil {
			return Result{}, err
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE release_group_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}
	mbBase := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	mbResolver, err := providercache.New(s.runtime, rgdomain.MusicBrainzNormalizerVersion, mbBase.Capability().RawRetention, mbBase.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	mbCollector := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, mbResolver)
	payloads, err := mbCollector.Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "release_group", Value: mbid})
	if err != nil {
		return Result{}, err
	}
	recorded, err := s.recordPayloads(ctx, payloads, rgdomain.MusicBrainzNormalizerVersion, mbCollector.Capability(), jobID)
	if err != nil || len(recorded) == 0 {
		if err == nil {
			err = fmt.Errorf("MusicBrainz returned no observations")
		}
		return Result{}, err
	}
	if recorded[0].Payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: recorded[0].Payload.StatusCode}
	}
	spine, err := musicbrainz.NormalizeReleaseGroup(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
	if err != nil {
		return Result{}, err
	}
	known := identifiersFromReleaseGroup(spine)
	known = append(known, providers.Identifier{Provider: "musicbrainz", Namespace: "release_group", Value: mbid})
	completed := map[string]bool{"musicbrainz": true}
	records := []rgdomain.NormalizedRecordV1{spine}
	failures := map[string]error{}
	if len(spine.ArtistCredits) > 0 && spine.ArtistCredits[0].ArtistProvider == "musicbrainz" && (credentials.APIKey("fanart") != "" || s.runtime.Config.Providers.Fanart.APIKey != "") {
		artistMBID := spine.ArtistCredits[0].ArtistID
		fanartBase := fanart.New(s.runtime.Config.Providers.Fanart)
		fanartResolver, resolverErr := providercache.New(s.runtime, rgdomain.FanartNormalizerVersion, fanartBase.Capability().RawRetention, fanartBase.Capability().ResponseCache, jobID)
		if resolverErr != nil {
			return Result{}, resolverErr
		}
		fanartPayloads, collectErr := fanart.NewCached(s.runtime.Config.Providers.Fanart, fanartResolver, credentials.APIKey("fanart")).Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: artistMBID})
		if collectErr != nil {
			failures["fanart"] = collectErr
		} else {
			observations, recordErr := s.recordPayloads(ctx, fanartPayloads, rgdomain.FanartNormalizerVersion, fanartBase.Capability(), jobID)
			if recordErr != nil {
				failures["fanart"] = recordErr
			} else if len(observations) > 0 && observations[0].Payload.StatusCode == http.StatusOK {
				normalized, normalizeErr := fanart.NormalizeMusicReleaseGroup(observations[0].Payload.Body, mbid, observations[0].ID, observations[0].Payload.ObservedAt)
				if normalizeErr != nil {
					failures["fanart"] = normalizeErr
				} else if len(normalized.Images) > 0 {
					records = append(records, normalized)
				}
			}
		}
	}
	collectors := []providers.Collector{}
	for _, build := range []func() (providers.Collector, error){func() (providers.Collector, error) {
		c := coverartarchive.New(s.runtime.Config.Providers.CoverArt)
		r, e := providercache.New(s.runtime, rgdomain.CoverArtNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, jobID)
		return coverartarchive.NewCached(s.runtime.Config.Providers.CoverArt, r), e
	}, func() (providers.Collector, error) {
		c := wikidata.New(s.runtime.Config.Providers.Wikidata)
		r, e := providercache.New(s.runtime, rgdomain.WikidataNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, jobID)
		return wikidata.NewCached(s.runtime.Config.Providers.Wikidata, r), e
	}, func() (providers.Collector, error) {
		c := discogs.New(s.runtime.Config.Providers.Discogs)
		r, e := providercache.New(s.runtime, rgdomain.DiscogsNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, jobID)
		return discogs.NewCached(s.runtime.Config.Providers.Discogs, r, credentials.APIKey("discogs")), e
	}, func() (providers.Collector, error) {
		c := apple.New(s.runtime.Config.Providers.Apple)
		r, e := providercache.New(s.runtime, rgdomain.AppleNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, jobID)
		return apple.NewCached(s.runtime.Config.Providers.Apple, r, credentials.APIKey("apple")), e
	}, func() (providers.Collector, error) {
		c := deezer.New(s.runtime.Config.Providers.Deezer)
		r, e := providercache.New(s.runtime, rgdomain.DeezerNormalizerVersion, c.Capability().RawRetention, c.Capability().ResponseCache, jobID)
		return deezer.NewCached(s.runtime.Config.Providers.Deezer, r), e
	}} {
		collector, e := build()
		if e != nil {
			return Result{}, e
		}
		collectors = append(collectors, collector)
	}
	planner := mixer.New(collectors...)
	desired := []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeCredits, providers.ScopeArtwork}
	for pass := 0; pass < 2; pass++ {
		plan := planner.BuildAllAvailable(known, desired, completed)
		if len(plan.Steps) == 0 {
			break
		}
		for _, step := range plan.Steps {
			provider := step.Collector.Capability().Provider
			payloads, e := step.Collector.Collect(ctx, step.Identifier)
			if e != nil {
				failures[provider] = e
				completed[provider] = true
				continue
			}
			observations, e := s.recordPayloads(ctx, payloads, releaseGroupNormalizerVersion(provider), step.Collector.Capability(), jobID)
			if e != nil || len(observations) == 0 {
				if e == nil {
					e = fmt.Errorf("collector returned no observations")
				}
				failures[provider] = e
				completed[provider] = true
				continue
			}
			if observations[0].Payload.StatusCode == http.StatusNotFound {
				completed[provider] = true
				continue
			}
			if observations[0].Payload.StatusCode != http.StatusOK {
				failures[provider] = &providers.StatusError{Provider: provider, StatusCode: observations[0].Payload.StatusCode}
				completed[provider] = true
				continue
			}
			var normalized rgdomain.NormalizedRecordV1
			switch provider {
			case "coverartarchive":
				normalized, e = coverartarchive.NormalizeReleaseGroup(observations[0].Payload.Body, mbid, observations[0].ID, observations[0].Payload.ObservedAt)
			case "wikidata":
				normalized, e = wikidata.NormalizeReleaseGroup(observations[0].Payload.Body, step.Identifier.Value, observations[0].ID, observations[0].Payload.ObservedAt)
			case "discogs":
				normalized, e = discogs.NormalizeMaster(observations[0].Payload.Body, observations[0].ID, observations[0].Payload.ObservedAt)
			case "apple":
				normalized, e = apple.NormalizeAlbum(observations[0].Payload.Body, step.Identifier.Value, observations[0].ID, observations[0].Payload.ObservedAt)
			case "deezer":
				normalized, e = deezer.NormalizeAlbum(observations[0].Payload.Body, observations[0].ID, observations[0].Payload.ObservedAt)
			}
			completed[provider] = true
			if e != nil {
				failures[provider] = e
				continue
			}
			records = append(records, normalized)
			known = append(known, identifiersFromReleaseGroup(normalized)...)
		}
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
			if providers.HasHTTPStatus(failures[key], http.StatusNotFound) {
				slog.Debug("supplemental release group provider has no matching record", "provider", key, "mbid", mbid)
			} else {
				slog.Warn("supplemental release group provider failed", "provider", key, "mbid", mbid, "error", failures[key])
			}
		}
		records[0] = spine
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		id, e := s.recordNormalized(ctx, record)
		if e != nil {
			return Result{}, e
		}
		ids = append(ids, id)
	}
	result, err = s.merge(ctx, ids, records, jobID)
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

func identifiersFromReleaseGroup(record rgdomain.NormalizedRecordV1) []providers.Identifier {
	result := make([]providers.Identifier, 0, len(record.IdentityCandidates))
	for _, candidate := range record.IdentityCandidates {
		result = append(result, providers.Identifier{Provider: candidate.Provider, Namespace: candidate.Namespace, Value: candidate.NormalizedValue})
	}
	return result
}
func releaseGroupNormalizerVersion(provider string) string {
	switch provider {
	case "coverartarchive":
		return rgdomain.CoverArtNormalizerVersion
	case "wikidata":
		return rgdomain.WikidataNormalizerVersion
	case "discogs":
		return rgdomain.DiscogsNormalizerVersion
	case "apple":
		return rgdomain.AppleNormalizerVersion
	case "deezer":
		return rgdomain.DeezerNormalizerVersion
	}
	return provider + "-release-group/v1"
}
func (s *Service) recordPayloads(ctx context.Context, payloads []providers.Payload, version string, cap providers.Capability, jobID int64) ([]ingest.RecordedObservation, error) {
	out := make([]ingest.RecordedObservation, 0, len(payloads))
	for _, payload := range payloads {
		if payload.ObservationID != "" {
			out = append(out, ingest.RecordedObservation{ID: payload.ObservationID, Checksum: payload.BlobChecksum, Payload: payload})
			continue
		}
		value, err := ingest.RecordObservation(ctx, s.runtime, payload, version, cap.RawRetention, cap.ResponseCache, jobID)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
func (s *Service) recordNormalized(ctx context.Context, record rgdomain.NormalizedRecordV1) (string, error) {
	document, _ := json.Marshal(record)
	supporting, _ := json.Marshal(record.ProviderRecord.SupportingObservationIDs)
	warnings, _ := json.Marshal(record.Warnings)
	var id string
	err := s.runtime.DB.QueryRow(ctx, `INSERT INTO normalized_records (entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,supporting_observation_ids,normalizer_version,schema_version,document,warnings,partial_failure,observed_at) VALUES ('release_group',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (primary_observation_id,normalizer_version,schema_version) DO UPDATE SET document=EXCLUDED.document,warnings=EXCLUDED.warnings,partial_failure=EXCLUDED.partial_failure RETURNING id`, record.ProviderRecord.Provider, record.ProviderRecord.Namespace, record.ProviderRecord.Value, record.ProviderRecord.PrimaryObservationID, supporting, record.ProviderRecord.NormalizerVersion, record.ProviderRecord.SchemaVersion, document, warnings, record.PartialFailure, record.ProviderRecord.ObservedAt).Scan(&id)
	return id, err
}
func (s *Service) merge(ctx context.Context, normalizedIDs []string, successful []rgdomain.NormalizedRecordV1, jobID int64) (Result, error) {
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	entityIDs := map[string]bool{}
	var candidates []rgdomain.IdentityCandidate
	for _, record := range successful {
		for _, candidate := range record.IdentityCandidates {
			if candidate.Confidence < 1 {
				continue
			}
			candidates = append(candidates, candidate)
			var id string
			e := tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='release_group' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, candidate.Provider, candidate.Namespace, candidate.NormalizedValue).Scan(&id)
			if e == nil {
				entityIDs[id] = true
			} else if e != pgx.ErrNoRows {
				return Result{}, e
			}
		}
	}
	if len(entityIDs) > 1 {
		claims, _ := json.Marshal(candidates)
		_, _ = tx.Exec(ctx, `INSERT INTO external_id_conflicts (entity_kind,claims,normalized_record_id) VALUES ('release_group',$1,$2)`, claims, normalizedIDs[0])
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("release group claims resolve to multiple canonical entities")
	}
	entityID := ""
	created := false
	for id := range entityIDs {
		entityID = id
	}
	if entityID == "" {
		created = true
		base := releaseGroupSlug(preferredTitle(successful[0]), firstYear(successful[0]))
		for n := 0; ; n++ {
			slug := base
			if n > 0 {
				slug = fmt.Sprintf("%s-%d", base, n+1)
			}
			e := tx.QueryRow(ctx, `INSERT INTO entities (kind,slug) VALUES ('release_group',$1) ON CONFLICT DO NOTHING RETURNING id`, slug).Scan(&entityID)
			if e == nil {
				_, e = tx.Exec(ctx, `INSERT INTO entity_slugs (entity_id,kind,slug) VALUES ($1,'release_group',$2)`, entityID, slug)
				if e != nil {
					return Result{}, e
				}
				break
			}
			if e != pgx.ErrNoRows {
				return Result{}, e
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
			tag, e := tx.Exec(ctx, `INSERT INTO external_id_claims (entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at) VALUES ($1,'release_group',$2,$3,$4,'accepted',$5,$6,$7,$7) ON CONFLICT (entity_kind,provider,namespace,normalized_value) DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, candidate.Provider, candidate.Namespace, candidate.NormalizedValue, candidate.Confidence, record.ProviderRecord.PrimaryObservationID, record.ProviderRecord.ObservedAt)
			if e != nil {
				return Result{}, e
			}
			if tag.RowsAffected() == 0 {
				return Result{}, fmt.Errorf("external ID belongs to another release group")
			}
		}
	}
	for _, id := range normalizedIDs {
		if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, id); err != nil {
			return Result{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (provider,provider_namespace,provider_record_id) id,document FROM normalized_records WHERE entity_id=$1 AND entity_kind='release_group' ORDER BY provider,provider_namespace,provider_record_id,observed_at DESC`, entityID)
	if err != nil {
		return Result{}, err
	}
	var records []rgdomain.RecordInput
	for rows.Next() {
		var id string
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			return Result{}, err
		}
		var record rgdomain.NormalizedRecordV1
		if err := json.Unmarshal(body, &record); err != nil {
			return Result{}, err
		}
		records = append(records, rgdomain.RecordInput{ID: id, Record: record})
	}
	rows.Close()
	imageIDs := map[string]string{}
	for _, input := range records {
		for _, image := range input.Record.Images {
			if image.SourceURL == "" {
				continue
			}
			providerID := image.ProviderImageID
			if providerID == "" {
				sum := sha256.Sum256([]byte(image.SourceURL))
				providerID = hex.EncodeToString(sum[:8])
			}
			var imageID string
			if err := tx.QueryRow(ctx, `INSERT INTO image_candidates (entity_id,provider,provider_image_id,class,source_url,language,width,height,provider_score,source_observation_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (entity_id,provider,provider_image_id,class) DO UPDATE SET source_url=EXCLUDED.source_url,language=EXCLUDED.language,width=EXCLUDED.width,height=EXCLUDED.height,provider_score=EXCLUDED.provider_score,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, input.Record.ProviderRecord.Provider, providerID, image.Class, image.SourceURL, image.Language, image.Width, image.Height, image.ProviderScore, input.Record.ProviderRecord.PrimaryObservationID).Scan(&imageID); err != nil {
				return Result{}, err
			}
			imageIDs[rgdomain.ImageKey(input.Record.ProviderRecord.Provider, image)] = imageID
		}
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now(),deleted_at=NULL WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	projection := rgdomain.Combine(entityID, slug, version, records, imageIDs, time.Now().UTC())
	if err := hydrateCanonicalIDs(ctx, tx, &projection.Detail); err != nil {
		return Result{}, err
	}
	detailJSON, _ := json.Marshal(projection.Detail)
	summaryJSON, _ := json.Marshal(projection.Summary)
	provenanceJSON, _ := json.Marshal(projection.Detail.Provenance)
	sourceJSON, _ := json.Marshal(records)
	digest := sha256.Sum256(append([]byte(rgdomain.MergeVersion+":"), sourceJSON...))
	if _, err := tx.Exec(ctx, `INSERT INTO canonical_release_groups (entity_id,merge_version,source_fingerprint,document) VALUES ($1,$2,$3,$4) ON CONFLICT (entity_id) DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, rgdomain.MergeVersion, hex.EncodeToString(digest[:]), detailJSON); err != nil {
		return Result{}, err
	}
	for _, document := range []struct {
		kind string
		body []byte
	}{{"detail", detailJSON}, {"summary", summaryJSON}} {
		if _, err := tx.Exec(ctx, `INSERT INTO api_documents (entity_id,document_kind,schema_version,projection_version,document,fresh_until) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (entity_id,document_kind) DO UPDATE SET schema_version=EXCLUDED.schema_version,projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, document.kind, rgdomain.ProjectionSchemaVersion, version, document.body, projection.Detail.Freshness.FreshUntil); err != nil {
			return Result{}, err
		}
	}
	_, _ = tx.Exec(ctx, `INSERT INTO api_document_provenance (entity_id,document_kind,projection_version,document) VALUES ($1,'detail',$2,$3) ON CONFLICT (entity_id,document_kind) DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document`, entityID, version, provenanceJSON)
	genres := projection.Summary.Genres
	if genres == nil {
		genres = []string{}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_entities (entity_id,kind,slug,display_title,release_year,status,genres,countries,languages,summary,projection_version) VALUES ($1,'release_group',$2,$3,NULLIF($4,0),$5,$6,'{}','{}',$7,$8) ON CONFLICT (entity_id) DO UPDATE SET slug=EXCLUDED.slug,display_title=EXCLUDED.display_title,release_year=EXCLUDED.release_year,status=EXCLUDED.status,genres=EXCLUDED.genres,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, projection.Detail.Display.Title, projection.Detail.Display.Year, projection.Detail.Data.Classification.PrimaryType, genres, summaryJSON, version); err != nil {
		return Result{}, err
	}
	for _, input := range records {
		for _, edition := range input.Record.Editions {
			if edition.Provider == "" || edition.Namespace == "" || edition.ProviderID == "" {
				continue
			}
			metadata, _ := json.Marshal(map[string]any{
				"title": edition.Title, "status": edition.Status, "date": edition.Date,
				"country": edition.Country, "barcode": edition.Barcode,
				"track_count": edition.TrackCount, "formats": edition.Formats,
			})
			if _, err := tx.Exec(ctx, `INSERT INTO entity_relations(source_entity_id,source_kind,target_kind,relation_type,provider,namespace,provider_value,metadata,state,source_observation_id,first_observed_at,last_observed_at) VALUES($1,'release_group','release','editions',$2,$3,$4,$5,'accepted',$6,$7,$7) ON CONFLICT(source_entity_id,relation_type,provider,namespace,provider_value) DO UPDATE SET metadata=EXCLUDED.metadata,state='accepted',source_observation_id=EXCLUDED.source_observation_id,last_observed_at=EXCLUDED.last_observed_at`, entityID, edition.Provider, edition.Namespace, edition.ProviderID, metadata, input.Record.ProviderRecord.PrimaryObservationID, input.Record.ProviderRecord.ObservedAt); err != nil {
				return Result{}, fmt.Errorf("persist release edition relation: %w", err)
			}
		}
	}
	for _, candidate := range candidates {
		if _, err := tx.Exec(ctx, `UPDATE entity_relations SET target_entity_id=$1 WHERE target_kind='release_group' AND provider=$2 AND namespace=$3 AND provider_value=$4 AND state='accepted'`, entityID, candidate.Provider, candidate.Namespace, candidate.NormalizedValue); err != nil {
			return Result{}, fmt.Errorf("link release group relations: %w", err)
		}
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID)
	for _, title := range projection.SearchTitles {
		if title.Value != "" {
			_, _ = tx.Exec(ctx, `INSERT INTO search_names (entity_id,value,normalized_value,locale,name_type,source_quality) VALUES ($1,$2,lower(unaccent($2)),$3,$4,$5) ON CONFLICT DO NOTHING`, entityID, title.Value, title.Language, title.Type, 80)
		}
	}
	for _, record := range successful {
		_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states (entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at) VALUES ($1,$2,now(),now(),$3,$4,$5) ON CONFLICT (entity_id,provider) DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at`, entityID, record.ProviderRecord.Provider, record.ProviderRecord.PrimaryObservationID, nullableJob(jobID), projection.Detail.Freshness.FreshUntil)
	}
	changeType := "updated"
	if created {
		changeType = "created"
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox (entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id) VALUES ($1,'release_group',$2,$3,$4,$5,$6,$7)`, entityID, slug, changeType, []string{"identity", "detail", "search", "provenance"}, version, successful[0].ProviderRecord.PrimaryObservationID, nullableJob(jobID))
	if jobID > 0 {
		_, _ = tx.Exec(ctx, `UPDATE release_group_ingestion_runs SET entity_id=$2,state='completed',completed_at=now(),error=NULL WHERE river_job_id=$1`, jobID, entityID)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{EntityID: entityID, NormalizedID: normalizedIDs[0], ProjectionVersion: version, Detail: projection.Detail}, nil
}
func (s *Service) cache(ctx context.Context, result Result) error {
	body, _ := json.Marshal(result.Detail)
	ttl := time.Until(result.Detail.Freshness.FreshUntil)
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := s.runtime.Redis.Set(ctx, "heya:metadata:v1:api:entity:"+result.EntityID+":detail", body, ttl).Err(); err != nil {
		return err
	}
	return s.runtime.Redis.Publish(ctx, "heya:metadata:v1:cache-invalidations", result.EntityID).Err()
}
func (s *Service) Detail(ctx context.Context, id string) (rgdomain.DetailDocument, bool, error) {
	key := "heya:metadata:v1:api:entity:" + id + ":detail"
	if body, err := s.runtime.Redis.Get(ctx, key).Bytes(); err == nil {
		var document rgdomain.DetailDocument
		if json.Unmarshal(body, &document) == nil && document.Kind == "release_group" {
			if err := hydrateCanonicalIDs(ctx, s.runtime.DB, &document); err != nil {
				return rgdomain.DetailDocument{}, false, err
			}
			return document, time.Now().Before(document.Freshness.FreshUntil), nil
		}
	}
	var body []byte
	var fresh time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, id).Scan(&body, &fresh); err == pgx.ErrNoRows {
		return rgdomain.DetailDocument{}, false, ErrNotFound
	} else if err != nil {
		return rgdomain.DetailDocument{}, false, err
	}
	var document rgdomain.DetailDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return document, false, err
	}
	if err := hydrateCanonicalIDs(ctx, s.runtime.DB, &document); err != nil {
		return rgdomain.DetailDocument{}, false, err
	}
	return document, time.Now().Before(fresh), nil
}

func hydrateCanonicalIDs(ctx context.Context, db canonicalrefs.Querier, document *rgdomain.DetailDocument) error {
	artistRefs := make([]canonicalrefs.Ref, 0, len(document.Data.ArtistCredits))
	for _, credit := range document.Data.ArtistCredits {
		artistRefs = append(artistRefs, canonicalrefs.Ref{Provider: credit.ArtistProvider, Namespace: credit.ArtistNamespace, Value: credit.ArtistID})
	}
	for _, track := range document.Data.Tracks {
		for _, credit := range track.ArtistCredits {
			artistRefs = append(artistRefs, canonicalrefs.Ref{Provider: credit.ArtistProvider, Namespace: credit.ArtistNamespace, Value: credit.ArtistID})
		}
	}
	artists, err := canonicalrefs.Resolve(ctx, db, "artist", artistRefs)
	if err != nil {
		return err
	}
	hydrateCredit := func(credit *rgdomain.ArtistCredit) {
		ref := canonicalrefs.Ref{Provider: credit.ArtistProvider, Namespace: credit.ArtistNamespace, Value: credit.ArtistID}
		credit.ArtistEntityID = artists[canonicalrefs.Key(ref)]
		credit.ResolutionState = "unresolved"
		if credit.ArtistEntityID != "" {
			credit.ResolutionState = "materialized"
		}
	}
	for index := range document.Data.ArtistCredits {
		hydrateCredit(&document.Data.ArtistCredits[index])
	}

	recordingRefs := make([]canonicalrefs.Ref, 0, len(document.Data.Tracks))
	for _, track := range document.Data.Tracks {
		ref := canonicalrefs.Ref{Provider: track.Provider, Namespace: "track", Value: track.ProviderID}
		if track.RecordingProvider != "" && track.RecordingID != "" {
			ref = canonicalrefs.Ref{Provider: track.RecordingProvider, Namespace: "recording", Value: track.RecordingID}
		}
		recordingRefs = append(recordingRefs, ref)
	}
	recordingEntities, err := canonicalrefs.Resolve(ctx, db, "recording", recordingRefs)
	if err != nil {
		return err
	}
	recordingIDs := make([]string, 0, len(document.Data.Tracks))
	for trackIndex := range document.Data.Tracks {
		track := &document.Data.Tracks[trackIndex]
		ref := canonicalrefs.Ref{Provider: track.Provider, Namespace: "track", Value: track.ProviderID}
		if track.RecordingProvider != "" && track.RecordingID != "" {
			ref = canonicalrefs.Ref{Provider: track.RecordingProvider, Namespace: "recording", Value: track.RecordingID}
		}
		track.RecordingEntityID = recordingEntities[canonicalrefs.Key(ref)]
		track.RecordingResolutionState = "unresolved"
		if track.RecordingEntityID != "" {
			track.RecordingResolutionState = "materialized"
		}
		track.LyricsAvailable = false
		recordingIDs = append(recordingIDs, track.RecordingEntityID)
		for creditIndex := range track.ArtistCredits {
			hydrateCredit(&track.ArtistCredits[creditIndex])
		}
	}
	lyricsAvailability, err := recordings.LyricsAvailability(ctx, db, recordingIDs)
	if err != nil {
		return err
	}
	for trackIndex := range document.Data.Tracks {
		track := &document.Data.Tracks[trackIndex]
		track.LyricsAvailable = lyricsAvailability[track.RecordingEntityID]
	}

	editionRefs := make([]canonicalrefs.Ref, 0, len(document.Data.Editions))
	for _, edition := range document.Data.Editions {
		editionRefs = append(editionRefs, canonicalrefs.Ref{Provider: edition.Provider, Namespace: edition.Namespace, Value: edition.ProviderID})
	}
	editions, err := canonicalrefs.Resolve(ctx, db, "release", editionRefs)
	if err != nil {
		return err
	}
	for index := range document.Data.Editions {
		edition := &document.Data.Editions[index]
		ref := canonicalrefs.Ref{Provider: edition.Provider, Namespace: edition.Namespace, Value: edition.ProviderID}
		edition.EntityID = editions[canonicalrefs.Key(ref)]
		edition.ResolutionState = "unresolved"
		if edition.EntityID != "" {
			edition.ResolutionState = "materialized"
		}
	}
	return nil
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	provider = strings.ToLower(provider)
	namespace = strings.ToLower(namespace)
	value = strings.TrimSpace(value)
	if provider == "wikidata" {
		value = strings.ToUpper(value)
	} else if provider == "musicbrainz" {
		value = strings.ToLower(value)
	}
	var id string
	if err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='release_group' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, provider, namespace, value).Scan(&id); err == pgx.ErrNoRows {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	return id, nil
}
func (s *Service) MusicBrainzID(ctx context.Context, entityID string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='release_group' AND provider='musicbrainz' AND namespace='release_group' AND state='accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return value, err
}
func preferredTitle(record rgdomain.NormalizedRecordV1) string {
	for _, title := range record.Titles {
		if title.Primary {
			return title.Value
		}
	}
	return "release-group"
}
func firstYear(record rgdomain.NormalizedRecordV1) int {
	for _, date := range record.Dates {
		if len(date.Value) >= 4 {
			var year int
			fmt.Sscanf(date.Value[:4], "%d", &year)
			if year > 0 {
				return year
			}
		}
	}
	return 0
}
func releaseGroupSlug(title string, year int) string {
	value := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if value == "" {
		value = "release-group"
	}
	if year > 0 {
		value += fmt.Sprintf("-%d", year)
	}
	return value
}
func nullableJob(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
