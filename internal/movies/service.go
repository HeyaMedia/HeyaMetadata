// Package movies orchestrates movie collection, normalization, identity, and projection.
package movies

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
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
	"github.com/HeyaMedia/HeyaMetadata/internal/ingest"
	"github.com/HeyaMedia/HeyaMetadata/internal/mixer"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/fanart"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/omdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
	"github.com/jackc/pgx/v5"
)

const changeSequencerLock int64 = 0x4845594143484745 // "HEYACHGE"

var slugNonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

type Result struct {
	EntityID          string
	NormalizedID      string
	ProjectionVersion int64
	Detail            moviedomain.DetailDocument
}

type Service struct {
	runtime *platform.Runtime
}

func NewService(runtime *platform.Runtime) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) IngestTMDB(ctx context.Context, tmdbID int64, riverJobID int64) (result Result, returnErr error) {
	return s.IngestTMDBWithCredentials(ctx, tmdbID, riverJobID, providercredentials.Credentials{})
}

func (s *Service) IngestTMDBWithCredentials(ctx context.Context, tmdbID int64, riverJobID int64, credentials providercredentials.Credentials) (result Result, returnErr error) {
	if tmdbID < 1 {
		return Result{}, fmt.Errorf("TMDB movie ID must be positive")
	}
	if riverJobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `
            INSERT INTO movie_ingestion_runs (river_job_id, tmdb_id, state)
            VALUES ($1, $2, 'working')
            ON CONFLICT (river_job_id) DO UPDATE SET state = 'working', error = NULL`, riverJobID, tmdbID); err != nil {
			return Result{}, fmt.Errorf("start movie ingestion run: %w", err)
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `
                    UPDATE movie_ingestion_runs
                    SET state = 'failed', error = $2, completed_at = now()
                    WHERE river_job_id = $1`, riverJobID, returnErr.Error())
			}
		}()
	}

	identifier := providers.Identifier{Provider: "tmdb", Namespace: "movie", Value: strconv.FormatInt(tmdbID, 10)}
	desired := []providers.Scope{
		providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
		providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
		providers.ScopeCredits, providers.ScopeArtwork, providers.ScopeCollections,
		providers.ScopeRecommendations,
	}
	tmdbCapability := tmdb.New(s.runtime.Config.Providers.TMDB).Capability()
	tmdbResolver, err := providercache.New(s.runtime, moviedomain.TMDBNormalizerVersion, tmdbCapability.RawRetention, tmdbCapability.ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	omdbCapability := omdb.New(s.runtime.Config.Providers.OMDB).Capability()
	omdbResolver, err := providercache.New(s.runtime, moviedomain.OMDBNormalizerVersion, omdbCapability.RawRetention, omdbCapability.ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	fanartCapability := fanart.New(s.runtime.Config.Providers.Fanart).Capability()
	fanartResolver, err := providercache.New(s.runtime, moviedomain.FanartNormalizerVersion, fanartCapability.RawRetention, fanartCapability.ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	tvdbCapability := tvdb.New(s.runtime.Config.Providers.TVDB).Capability()
	tvdbResolver, err := providercache.New(s.runtime, moviedomain.TVDBNormalizerVersion, tvdbCapability.RawRetention, tvdbCapability.ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	planner := mixer.New(
		tmdb.NewCached(s.runtime.Config.Providers.TMDB, tmdbResolver, credentials.APIKey("tmdb")),
		omdb.NewCached(s.runtime.Config.Providers.OMDB, omdbResolver, credentials.APIKey("omdb")),
		fanart.NewCached(s.runtime.Config.Providers.Fanart, fanartResolver, credentials.APIKey("fanart")),
		tvdb.NewCached(s.runtime.Config.Providers.TVDB, tvdbResolver, credentials.APIKey("tvdb"), s.runtime.Redis),
	)
	knownIdentifiers := []providers.Identifier{identifier}
	completed := map[string]bool{}
	plan := planner.BuildAvailable(knownIdentifiers, desired, completed)
	if len(plan.Steps) == 0 {
		return Result{}, fmt.Errorf("no provider collector accepts TMDB movie IDs")
	}
	tmdbStep := plan.Steps[0]
	payloads, err := tmdbStep.Collector.Collect(ctx, tmdbStep.Identifier)
	if err != nil {
		return Result{}, err
	}
	recorded, err := s.recordPayloads(ctx, payloads, moviedomain.TMDBNormalizerVersion, tmdbStep.Collector.Capability(), riverJobID)
	if err != nil {
		return Result{}, err
	}
	if len(recorded) == 0 {
		return Result{}, fmt.Errorf("TMDB collector returned no observations")
	}
	if recorded[0].Payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "tmdb", StatusCode: recorded[0].Payload.StatusCode}
	}
	var collectionBody []byte
	var supportingIDs []string
	if len(recorded) > 1 {
		supportingIDs = append(supportingIDs, recorded[1].ID)
		if recorded[1].Payload.StatusCode == http.StatusOK {
			collectionBody = recorded[1].Payload.Body
		}
	}
	tmdbNormalized, err := tmdb.Normalize(recorded[0].Payload.Body, collectionBody, recorded[0].ID, supportingIDs, recorded[0].Payload.ObservedAt, s.runtime.Config.Providers.TMDB.Language)
	if err != nil {
		return Result{}, err
	}
	completed["tmdb"] = true
	for _, candidate := range tmdbNormalized.IdentityCandidates {
		knownIdentifiers = append(knownIdentifiers, providers.Identifier{Provider: candidate.Provider, Namespace: candidate.Namespace, Value: candidate.NormalizedValue})
	}

	var supplemental []moviedomain.NormalizedRecordV1
	var omdbFailure error
	var fanartFailure error
	var tvdbFailure error
	followUp := planner.BuildAvailable(knownIdentifiers, desired, completed)
	for _, step := range followUp.Steps {
		switch step.Collector.Capability().Provider {
		case "omdb":
			providerPayloads, collectErr := step.Collector.Collect(ctx, step.Identifier)
			if collectErr != nil {
				omdbFailure = collectErr
				continue
			}
			providerRecorded, recordErr := s.recordPayloads(ctx, providerPayloads, moviedomain.OMDBNormalizerVersion, step.Collector.Capability(), riverJobID)
			if recordErr != nil || len(providerRecorded) == 0 {
				omdbFailure = firstError(recordErr, "OMDb collector returned no observations")
				continue
			}
			if providerRecorded[0].Payload.StatusCode != http.StatusOK {
				omdbFailure = &providers.StatusError{Provider: "omdb", StatusCode: providerRecorded[0].Payload.StatusCode}
				continue
			}
			if message, failed := omdb.ResponseError(providerRecorded[0].Payload.Body); failed {
				omdbFailure = fmt.Errorf("OMDb response: %s", message)
				continue
			}
			normalized, normalizeErr := omdb.Normalize(providerRecorded[0].Payload.Body, providerRecorded[0].ID, providerRecorded[0].Payload.ObservedAt)
			if normalizeErr != nil {
				omdbFailure = normalizeErr
				continue
			}
			supplemental = append(supplemental, normalized)
			completed["omdb"] = true
		case "tvdb":
			providerPayloads, collectErr := step.Collector.Collect(ctx, step.Identifier)
			if collectErr != nil {
				tvdbFailure = collectErr
				continue
			}
			providerRecorded, recordErr := s.recordPayloads(ctx, providerPayloads, moviedomain.TVDBNormalizerVersion, step.Collector.Capability(), riverJobID)
			if recordErr != nil || len(providerRecorded) < 2 {
				tvdbFailure = firstError(recordErr, "TVDB has no movie for the IMDb title ID")
				continue
			}
			for _, observation := range providerRecorded {
				if observation.Payload.StatusCode != http.StatusOK {
					tvdbFailure = &providers.StatusError{Provider: "tvdb", StatusCode: observation.Payload.StatusCode}
					break
				}
			}
			if tvdbFailure != nil {
				continue
			}
			detail := providerRecorded[len(providerRecorded)-1]
			supporting := make([]string, 0, len(providerRecorded)-1)
			for _, observation := range providerRecorded[:len(providerRecorded)-1] {
				supporting = append(supporting, observation.ID)
			}
			normalized, normalizeErr := tvdb.Normalize(detail.Payload.Body, detail.ID, supporting, detail.Payload.ObservedAt)
			if normalizeErr != nil {
				tvdbFailure = normalizeErr
				continue
			}
			supplemental = append(supplemental, normalized)
			completed["tvdb"] = true
		case "fanart":
			providerPayloads, collectErr := step.Collector.Collect(ctx, step.Identifier)
			if collectErr != nil {
				fanartFailure = collectErr
				continue
			}
			providerRecorded, recordErr := s.recordPayloads(ctx, providerPayloads, moviedomain.FanartNormalizerVersion, step.Collector.Capability(), riverJobID)
			if recordErr != nil || len(providerRecorded) == 0 {
				fanartFailure = firstError(recordErr, "Fanart.tv collector returned no observations")
				continue
			}
			if providerRecorded[0].Payload.StatusCode != http.StatusOK {
				fanartFailure = &providers.StatusError{Provider: "fanart", StatusCode: providerRecorded[0].Payload.StatusCode}
				continue
			}
			normalized, normalizeErr := fanart.Normalize(providerRecorded[0].Payload.Body, providerRecorded[0].ID, providerRecorded[0].Payload.ObservedAt)
			if normalizeErr != nil {
				fanartFailure = normalizeErr
				continue
			}
			supplemental = append(supplemental, normalized)
			completed["fanart"] = true
		}
	}
	if omdbFailure != nil {
		tmdbNormalized.PartialFailure = true
		tmdbNormalized.Warnings = append(tmdbNormalized.Warnings, "omdb: "+omdbFailure.Error())
		slog.Warn("supplemental movie provider failed", "provider", "omdb", "tmdb_id", tmdbID, "error", omdbFailure)
	}
	if tvdbFailure != nil {
		tmdbNormalized.PartialFailure = true
		tmdbNormalized.Warnings = append(tmdbNormalized.Warnings, "tvdb: "+tvdbFailure.Error())
		slog.Warn("supplemental movie provider failed", "provider", "tvdb", "tmdb_id", tmdbID, "error", tvdbFailure)
	}
	if fanartFailure != nil {
		tmdbNormalized.PartialFailure = true
		tmdbNormalized.Warnings = append(tmdbNormalized.Warnings, "fanart: "+fanartFailure.Error())
		slog.Warn("supplemental movie provider failed", "provider", "fanart", "tmdb_id", tmdbID, "error", fanartFailure)
	}

	normalizedID, err := s.recordNormalized(ctx, tmdbNormalized)
	if err != nil {
		return Result{}, err
	}
	additionalNormalizedIDs := make([]string, 0, len(supplemental))
	successfulRecords := []moviedomain.NormalizedRecordV1{tmdbNormalized}
	for _, normalized := range supplemental {
		id, recordErr := s.recordNormalized(ctx, normalized)
		if recordErr != nil {
			return Result{}, recordErr
		}
		additionalNormalizedIDs = append(additionalNormalizedIDs, id)
		successfulRecords = append(successfulRecords, normalized)
	}

	result, err = s.merge(ctx, normalizedID, additionalNormalizedIDs, tmdbNormalized, successfulRecords, riverJobID)
	if err != nil {
		return Result{}, err
	}
	if omdbFailure != nil {
		s.recordProviderFailure(ctx, result.EntityID, "omdb", omdbFailure)
	}
	if tvdbFailure != nil {
		s.recordProviderFailure(ctx, result.EntityID, "tvdb", tvdbFailure)
	}
	if fanartFailure != nil {
		s.recordProviderFailure(ctx, result.EntityID, "fanart", fanartFailure)
	}
	if err := s.cache(ctx, result); err != nil {
		return Result{}, err
	}
	if err := s.SequenceChanges(ctx, 100); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Service) recordPayloads(ctx context.Context, payloads []providers.Payload, normalizerVersion string, capability providers.Capability, riverJobID int64) ([]ingest.RecordedObservation, error) {
	recorded := make([]ingest.RecordedObservation, 0, len(payloads))
	for _, payload := range payloads {
		if payload.ObservationID != "" {
			recorded = append(recorded, ingest.RecordedObservation{ID: payload.ObservationID, Checksum: payload.BlobChecksum, Payload: payload})
			continue
		}
		observation, err := ingest.RecordObservation(ctx, s.runtime, payload, normalizerVersion, capability.RawRetention, capability.ResponseCache, riverJobID)
		if err != nil {
			return nil, err
		}
		recorded = append(recorded, observation)
	}
	return recorded, nil
}

func firstError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", fallback)
}

func (s *Service) recordNormalized(ctx context.Context, normalized moviedomain.NormalizedRecordV1) (string, error) {
	document, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode normalized %s movie: %w", normalized.ProviderRecord.Provider, err)
	}
	supporting, _ := json.Marshal(normalized.ProviderRecord.SupportingObservationIDs)
	warnings, _ := json.Marshal(normalized.Warnings)
	var id string
	if err := s.runtime.DB.QueryRow(ctx, `
		INSERT INTO normalized_records (
			entity_kind, provider, provider_namespace, provider_record_id,
			primary_observation_id, supporting_observation_ids, normalizer_version,
			schema_version, document, warnings, partial_failure, observed_at
		) VALUES ('movie', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (primary_observation_id, normalizer_version, schema_version)
		DO UPDATE SET document = EXCLUDED.document, warnings = EXCLUDED.warnings,
		              partial_failure = EXCLUDED.partial_failure
		RETURNING id`,
		normalized.ProviderRecord.Provider, normalized.ProviderRecord.Namespace, normalized.ProviderRecord.Value,
		normalized.ProviderRecord.PrimaryObservationID, supporting, normalized.ProviderRecord.NormalizerVersion,
		normalized.ProviderRecord.SchemaVersion, document, warnings, normalized.PartialFailure, normalized.ProviderRecord.ObservedAt,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("record normalized %s movie: %w", normalized.ProviderRecord.Provider, err)
	}
	return id, nil
}

func (s *Service) merge(ctx context.Context, normalizedID string, additionalNormalizedIDs []string, normalized moviedomain.NormalizedRecordV1, successfulRecords []moviedomain.NormalizedRecordV1, riverJobID int64) (Result, error) {
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin movie merge: %w", err)
	}
	defer tx.Rollback(ctx)

	entityIDs := map[string]bool{}
	var allCandidates []moviedomain.IdentityCandidate
	for _, successful := range successfulRecords {
		allCandidates = append(allCandidates, successful.IdentityCandidates...)
		for _, candidate := range successful.IdentityCandidates {
			var entityID string
			err := tx.QueryRow(ctx, `
            SELECT entity_id FROM external_id_claims
            WHERE entity_kind = 'movie' AND provider = $1 AND namespace = $2
              AND normalized_value = $3 AND state = 'accepted'`,
				candidate.Provider, candidate.Namespace, candidate.NormalizedValue,
			).Scan(&entityID)
			if err == nil {
				entityIDs[entityID] = true
			} else if err != pgx.ErrNoRows {
				return Result{}, fmt.Errorf("resolve movie identity: %w", err)
			}
		}
	}
	if len(entityIDs) > 1 {
		claims, _ := json.Marshal(allCandidates)
		if _, err := tx.Exec(ctx, `INSERT INTO external_id_conflicts (entity_kind, claims, normalized_record_id) VALUES ('movie', $1, $2)`, claims, normalizedID); err != nil {
			return Result{}, fmt.Errorf("record movie identity conflict: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("TMDB movie claims resolve to multiple canonical movies")
	}

	entityID := ""
	created := false
	for id := range entityIDs {
		entityID = id
	}
	if entityID == "" {
		created = true
		baseSlug := movieSlug(preferredTitle(normalized), recordYear(normalized))
		for suffix := 0; ; suffix++ {
			slug := baseSlug
			if suffix > 0 {
				slug = fmt.Sprintf("%s-%d", baseSlug, suffix+1)
			}
			err := tx.QueryRow(ctx, `INSERT INTO entities (kind, slug) VALUES ('movie', $1) ON CONFLICT DO NOTHING RETURNING id`, slug).Scan(&entityID)
			if err == nil {
				if _, err := tx.Exec(ctx, `INSERT INTO entity_slugs (entity_id, kind, slug) VALUES ($1, 'movie', $2)`, entityID, slug); err != nil {
					return Result{}, err
				}
				break
			}
			if err != pgx.ErrNoRows {
				return Result{}, fmt.Errorf("create canonical movie: %w", err)
			}
		}
	}
	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id = $1 FOR UPDATE`, entityID).Scan(&slug); err != nil {
		return Result{}, fmt.Errorf("lock canonical movie: %w", err)
	}
	for _, successful := range successfulRecords {
		for _, candidate := range successful.IdentityCandidates {
			commandTag, err := tx.Exec(ctx, `
            INSERT INTO external_id_claims (
                entity_id, entity_kind, provider, namespace, normalized_value,
                state, confidence, source_observation_id, first_observed_at, last_observed_at
            ) VALUES ($1, 'movie', $2, $3, $4, 'accepted', $5, $6, $7, $7)
            ON CONFLICT (entity_kind, provider, namespace, normalized_value)
            DO UPDATE SET last_observed_at = EXCLUDED.last_observed_at,
                          source_observation_id = EXCLUDED.source_observation_id
            WHERE external_id_claims.entity_id = EXCLUDED.entity_id`,
				entityID, candidate.Provider, candidate.Namespace, candidate.NormalizedValue,
				candidate.Confidence, successful.ProviderRecord.PrimaryObservationID, successful.ProviderRecord.ObservedAt,
			)
			if err != nil {
				return Result{}, fmt.Errorf("attach external ID claim: %w", err)
			}
			if commandTag.RowsAffected() == 0 {
				return Result{}, fmt.Errorf("external ID %s.%s:%s belongs to another movie", candidate.Provider, candidate.Namespace, candidate.NormalizedValue)
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id = $1 WHERE id = $2`, entityID, normalizedID); err != nil {
		return Result{}, fmt.Errorf("attach normalized record: %w", err)
	}
	for _, additionalID := range additionalNormalizedIDs {
		if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id = $1 WHERE id = $2`, entityID, additionalID); err != nil {
			return Result{}, fmt.Errorf("attach supplemental normalized record: %w", err)
		}
	}

	rows, err := tx.Query(ctx, `
        SELECT DISTINCT ON (provider, provider_namespace, provider_record_id)
               id, document
        FROM normalized_records
        WHERE entity_id = $1 AND entity_kind = 'movie'
        ORDER BY provider, provider_namespace, provider_record_id, observed_at DESC`, entityID)
	if err != nil {
		return Result{}, fmt.Errorf("load movie source records: %w", err)
	}
	var records []moviedomain.RecordInput
	for rows.Next() {
		var id string
		var document []byte
		if err := rows.Scan(&id, &document); err != nil {
			rows.Close()
			return Result{}, err
		}
		var record moviedomain.NormalizedRecordV1
		if err := json.Unmarshal(document, &record); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("decode normalized movie %s: %w", id, err)
		}
		records = append(records, moviedomain.RecordInput{ID: id, Record: record})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()

	imageIDs := map[string]string{}
	for _, input := range records {
		for _, image := range input.Record.Images {
			var imageID string
			if err := tx.QueryRow(ctx, `
                INSERT INTO image_candidates (
                    entity_id, provider, provider_image_id, class, source_url,
                    language, country, width, height, provider_score, source_observation_id
                ) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11)
                ON CONFLICT (entity_id, provider, provider_image_id, class)
                DO UPDATE SET width = EXCLUDED.width, height = EXCLUDED.height,
                              provider_score = EXCLUDED.provider_score,
                              source_observation_id = EXCLUDED.source_observation_id
                RETURNING id`, entityID, input.Record.ProviderRecord.Provider,
				image.ProviderImageID, image.Class, image.SourceURL, image.Language, image.Country,
				image.Width, image.Height, image.ProviderScore, input.Record.ProviderRecord.PrimaryObservationID,
			).Scan(&imageID); err != nil {
				return Result{}, fmt.Errorf("record image candidate: %w", err)
			}
			imageIDs[input.Record.ProviderRecord.Provider+":"+image.Class+":"+image.ProviderImageID] = imageID
		}
		auxiliary := auxiliaryImages(input.Record)
		for _, image := range auxiliary {
			var imageID string
			if err := tx.QueryRow(ctx, `
                INSERT INTO image_candidates (
                    entity_id, provider, provider_image_id, class, source_url,
                    language, country, width, height, provider_score, source_observation_id
                ) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11)
                ON CONFLICT (entity_id, provider, provider_image_id, class)
                DO UPDATE SET source_observation_id = EXCLUDED.source_observation_id
                RETURNING id`, entityID, input.Record.ProviderRecord.Provider,
				image.providerID, image.class, image.sourceURL, "", "", 0, 0, 0,
				input.Record.ProviderRecord.PrimaryObservationID).Scan(&imageID); err != nil {
				return Result{}, fmt.Errorf("record auxiliary image candidate: %w", err)
			}
			imageIDs[image.key] = imageID
		}
	}
	var projectionVersion int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version = canonical_version + 1, updated_at = now() WHERE id = $1 RETURNING canonical_version`, entityID).Scan(&projectionVersion); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	projection := moviedomain.Combine(entityID, slug, projectionVersion, records, imageIDs, now)
	detailJSON, _ := json.Marshal(projection.Detail)
	summaryJSON, _ := json.Marshal(projection.Summary)
	provenanceJSON, _ := json.Marshal(projection.Detail.Provenance)
	sourceJSON, _ := json.Marshal(records)
	fingerprintDigest := sha256.Sum256(append([]byte(moviedomain.MergeVersion+":"), sourceJSON...))
	fingerprint := hex.EncodeToString(fingerprintDigest[:])
	if _, err := tx.Exec(ctx, `
        INSERT INTO canonical_movies (entity_id, merge_version, source_fingerprint, document)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (entity_id) DO UPDATE SET merge_version = EXCLUDED.merge_version,
            source_fingerprint = EXCLUDED.source_fingerprint, document = EXCLUDED.document, updated_at = now()`,
		entityID, moviedomain.MergeVersion, fingerprint, detailJSON); err != nil {
		return Result{}, fmt.Errorf("write canonical movie: %w", err)
	}
	for _, document := range []struct {
		kind string
		body []byte
	}{{"detail", detailJSON}, {"summary", summaryJSON}} {
		if _, err := tx.Exec(ctx, `
            INSERT INTO api_documents (entity_id, document_kind, schema_version, projection_version, document, fresh_until)
            VALUES ($1, $2, $3, $4, $5, $6)
            ON CONFLICT (entity_id, document_kind) DO UPDATE SET
                schema_version = EXCLUDED.schema_version,
                projection_version = EXCLUDED.projection_version,
                document = EXCLUDED.document, fresh_until = EXCLUDED.fresh_until, updated_at = now()
            WHERE api_documents.projection_version <= EXCLUDED.projection_version`,
			entityID, document.kind, moviedomain.ProjectionSchemaVersion, projectionVersion, document.body, projection.Detail.Freshness.FreshUntil); err != nil {
			return Result{}, fmt.Errorf("write %s projection: %w", document.kind, err)
		}
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO api_document_provenance (entity_id, document_kind, projection_version, document)
        VALUES ($1, 'detail', $2, $3)
        ON CONFLICT (entity_id, document_kind) DO UPDATE SET projection_version = EXCLUDED.projection_version, document = EXCLUDED.document`, entityID, projectionVersion, provenanceJSON); err != nil {
		return Result{}, fmt.Errorf("write movie provenance: %w", err)
	}
	popularity := any(nil)
	if projection.Detail.Data.Measurements.Popularity != nil {
		popularity = *projection.Detail.Data.Measurements.Popularity
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO search_entities (entity_id, kind, slug, display_title, release_year, status, genres, countries, languages, popularity, summary, projection_version)
        VALUES ($1, 'movie', $2, $3, NULLIF($4, 0), $5, $6, $7, $8, $9, $10, $11)
        ON CONFLICT (entity_id) DO UPDATE SET slug = EXCLUDED.slug, display_title = EXCLUDED.display_title,
            release_year = EXCLUDED.release_year, status = EXCLUDED.status, genres = EXCLUDED.genres,
            countries = EXCLUDED.countries, languages = EXCLUDED.languages, popularity = EXCLUDED.popularity,
            summary = EXCLUDED.summary, projection_version = EXCLUDED.projection_version, updated_at = now()`,
		entityID, slug, projection.Detail.Display.Title, projection.Detail.Display.Year,
		projection.Detail.Data.Release.NormalizedStatus, projection.Detail.Data.Classification.Genres,
		projection.Detail.Data.Classification.Countries, projection.Detail.Data.Classification.SpokenLanguages,
		popularity, summaryJSON, projectionVersion); err != nil {
		return Result{}, fmt.Errorf("write movie search projection: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id = $1`, entityID); err != nil {
		return Result{}, err
	}
	for _, title := range projection.SearchNames {
		if strings.TrimSpace(title.Value) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
            INSERT INTO search_names (entity_id, value, normalized_value, locale, name_type, source_quality)
            VALUES ($1, $2, lower(unaccent($2)), $3, $4, $5)
            ON CONFLICT DO NOTHING`, entityID, title.Value, title.Language, title.Type, titleQuality(title.Type)); err != nil {
			return Result{}, fmt.Errorf("write movie search name: %w", err)
		}
	}
	for _, successful := range successfulRecords {
		providerRecord := successful.ProviderRecord
		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_refresh_states (entity_id, provider, last_attempt_at, last_success_at, last_observation_id, current_job_id, next_eligible_at)
			VALUES ($1, $2, now(), now(), $3, $4, $5)
			ON CONFLICT (entity_id, provider) DO UPDATE SET last_attempt_at = now(), last_success_at = now(),
				last_observation_id = EXCLUDED.last_observation_id, failure_class = NULL, failure_message = NULL,
				current_job_id = EXCLUDED.current_job_id, next_eligible_at = EXCLUDED.next_eligible_at`,
			entityID, providerRecord.Provider, providerRecord.PrimaryObservationID, nullableJobID(riverJobID), projection.Detail.Freshness.FreshUntil); err != nil {
			return Result{}, err
		}
	}
	changeType := "updated"
	if created {
		changeType = "created"
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO change_outbox (entity_id, entity_kind, slug, change_type, changed_scopes, projection_version, provider_observation_id, river_job_id)
        VALUES ($1, 'movie', $2, $3, $4, $5, $6, $7)`, entityID, slug, changeType,
		[]string{"identity", "detail", "search", "provenance"}, projectionVersion,
		normalized.ProviderRecord.PrimaryObservationID, nullableJobID(riverJobID)); err != nil {
		return Result{}, fmt.Errorf("write movie change outbox: %w", err)
	}
	if riverJobID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE movie_ingestion_runs SET entity_id = $2, state = 'completed', completed_at = now(), error = NULL WHERE river_job_id = $1`, riverJobID, entityID); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit movie merge: %w", err)
	}
	return Result{EntityID: entityID, NormalizedID: normalizedID, ProjectionVersion: projectionVersion, Detail: projection.Detail}, nil
}

func (s *Service) cache(ctx context.Context, result Result) error {
	body, err := json.Marshal(result.Detail)
	if err != nil {
		return err
	}
	key := "heya:metadata:v1:api:entity:" + result.EntityID + ":detail"
	ttl := time.Until(result.Detail.Freshness.FreshUntil)
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := s.runtime.Redis.Set(ctx, key, body, ttl).Err(); err != nil {
		return fmt.Errorf("cache movie detail: %w", err)
	}
	if err := s.runtime.Redis.Publish(ctx, "heya:metadata:v1:cache-invalidations", result.EntityID).Err(); err != nil {
		return fmt.Errorf("publish movie invalidation: %w", err)
	}
	return nil
}

func (s *Service) SequenceChanges(ctx context.Context, limit int) error {
	if limit < 1 {
		limit = 100
	}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, changeSequencerLock); err != nil {
		return err
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT last_sequence FROM change_cursor WHERE singleton = true FOR UPDATE`).Scan(&sequence); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
        SELECT id, entity_id, entity_kind, slug, scope, change_type, changed_scopes, projection_version, committed_at
        FROM change_outbox WHERE sequenced_at IS NULL
        ORDER BY committed_at, id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return err
	}
	type pending struct {
		id, entityID, kind, slug, scope, changeType string
		scopes                                      []string
		version                                     int64
		at                                          time.Time
	}
	var entries []pending
	for rows.Next() {
		var entry pending
		if err := rows.Scan(&entry.id, &entry.entityID, &entry.kind, &entry.slug, &entry.scope, &entry.changeType, &entry.scopes, &entry.version, &entry.at); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry)
	}
	rows.Close()
	for _, entry := range entries {
		sequence++
		if _, err := tx.Exec(ctx, `
            INSERT INTO change_log (sequence, outbox_id, entity_id, entity_kind, slug, scope, change_type, changed_scopes, projection_version, created_at)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, sequence, entry.id, entry.entityID, entry.kind, entry.slug, entry.scope, entry.changeType, entry.scopes, entry.version, entry.at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE change_outbox SET sequenced_at = now() WHERE id = $1`, entry.id); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE change_cursor SET last_sequence = $1 WHERE singleton = true`, sequence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func preferredTitle(record moviedomain.NormalizedRecordV1) string {
	for _, title := range record.Titles {
		if title.Type == "display" && title.Value != "" {
			return title.Value
		}
	}
	for _, title := range record.Titles {
		if title.Value != "" {
			return title.Value
		}
	}
	return "movie"
}
func recordYear(record moviedomain.NormalizedRecordV1) int {
	years := []int{}
	for _, event := range record.Lifecycle.ReleaseEvents {
		if len(event.Date) >= 4 {
			if value, err := strconv.Atoi(event.Date[:4]); err == nil {
				years = append(years, value)
			}
		}
	}
	sort.Ints(years)
	if len(years) > 0 {
		return years[0]
	}
	return 0
}
func movieSlug(title string, year int) string {
	slug := strings.Trim(slugNonAlphanumeric.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if slug == "" {
		slug = "movie"
	}
	if year > 0 {
		slug += "-" + strconv.Itoa(year)
	}
	return slug
}
func titleQuality(kind string) int {
	switch kind {
	case "display":
		return 100
	case "original":
		return 90
	case "translated":
		return 80
	default:
		return 50
	}
}

type auxiliaryImage struct{ key, providerID, class, sourceURL string }

func auxiliaryImages(record moviedomain.NormalizedRecordV1) []auxiliaryImage {
	provider := record.ProviderRecord.Provider
	var result []auxiliaryImage
	appendImage := func(scope, providerID, class, sourceURL string) {
		if sourceURL == "" {
			return
		}
		result = append(result, auxiliaryImage{key: moviedomain.AuxiliaryImageKey(provider, scope, providerID, sourceURL), providerID: scope + ":" + providerID + ":" + sourceURL, class: class, sourceURL: sourceURL})
	}
	for _, company := range record.Companies {
		appendImage("company", company.ProviderID, "logo", company.LogoURL)
	}
	for _, credit := range record.Credits {
		appendImage("credit", credit.ProviderPersonID, "profile", credit.ProfileURL)
	}
	if record.Collection != nil {
		for _, image := range record.Collection.Images {
			appendImage("collection_"+image.Class, record.Collection.ProviderID, "collection_"+image.Class, image.SourceURL)
		}
		for _, member := range record.Collection.Members {
			appendImage("collection_member", member.ProviderID, "poster", member.ImageURL)
		}
	}
	for _, recommendation := range record.Recommendations {
		appendImage("recommendation", recommendation.ProviderTargetID, "poster", recommendation.ImageURL)
	}
	return result
}
func nullableJobID(jobID int64) any {
	if jobID == 0 {
		return nil
	}
	return jobID
}

func (s *Service) recordProviderFailure(ctx context.Context, entityID, provider string, err error) {
	class := providerFailureClass(err)
	_, updateErr := s.runtime.DB.Exec(ctx, `
		INSERT INTO provider_refresh_states (entity_id, provider, last_attempt_at, failure_class, failure_message)
		VALUES ($1, $2, now(), $3, $4)
		ON CONFLICT (entity_id, provider) DO UPDATE SET
			last_attempt_at = now(), failure_class = EXCLUDED.failure_class,
			failure_message = EXCLUDED.failure_message`, entityID, provider, class, err.Error())
	if updateErr != nil {
		slog.Error("record refresh failure", "error", updateErr)
	}
}

func providerFailureClass(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not found") {
		return "not_found"
	}
	if strings.Contains(message, "api key") || strings.Contains(message, "unauthorized") || strings.Contains(message, "401") {
		return "authentication"
	}
	if strings.Contains(message, "rate") || strings.Contains(message, "429") {
		return "rate_limited"
	}
	return "provider_error"
}
