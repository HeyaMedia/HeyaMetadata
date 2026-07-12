// Package recordings ingests and reads canonical standalone music recordings.
package recordings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = fmt.Errorf("recording not found")
var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

type Result struct {
	EntityID          string
	NormalizedID      string
	ProjectionVersion int64
	Detail            releasedomain.RecordingDocument
}

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) IngestMusicBrainz(ctx context.Context, mbid string, jobID int64) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if jobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO recording_ingestion_runs(river_job_id,musicbrainz_id,state)VALUES($1,$2,'working')ON CONFLICT(river_job_id)DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, mbid); err != nil {
			return result, err
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE recording_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, releasedomain.RecordingNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver).Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "recording", Value: mbid})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("MusicBrainz returned no recording")
	}
	payload := payloads[0]
	if payload.StatusCode != 200 {
		return result, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
	}
	record, err := musicbrainz.NormalizeRecording(payload.Body, payload.ObservationID, payload.ObservedAt)
	if err != nil {
		return result, err
	}
	result, err = s.persist(ctx, record, jobID)
	if err != nil {
		return result, err
	}
	if err := changelog.Sequence(ctx, s.runtime, 100); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) persist(ctx context.Context, record releasedomain.NormalizedRecording, jobID int64) (Result, error) {
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	normalizedBody, _ := json.Marshal(record)
	var normalizedID string
	if err := tx.QueryRow(ctx, `INSERT INTO normalized_records(entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES('recording',$1,$2,$3,$4,$5,$6,$7,$8)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET document=EXCLUDED.document RETURNING id`, record.ProviderRecord.Provider, record.ProviderRecord.Namespace, record.ProviderRecord.Value, record.ProviderRecord.PrimaryObservationID, record.ProviderRecord.NormalizerVersion, record.ProviderRecord.SchemaVersion, normalizedBody, record.ProviderRecord.ObservedAt).Scan(&normalizedID); err != nil {
		return Result{}, err
	}
	entityID, slug, created, err := resolveOrCreate(ctx, tx, record.Recording.ProviderID, record.Recording.Title)
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, normalizedID); err != nil {
		return Result{}, err
	}
	if err := persistClaims(ctx, tx, entityID, record); err != nil {
		return Result{}, err
	}
	var existing releasedomain.RecordingDocument
	var existingBody []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM canonical_recordings WHERE entity_id=$1`, entityID).Scan(&existingBody); err == nil {
		_ = json.Unmarshal(existingBody, &existing)
	} else if err != pgx.ErrNoRows {
		return Result{}, err
	}
	recording := MergeData(existing.Data, record.Recording)
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	fresh := releasedomain.Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(7 * 24 * time.Hour), Providers: map[string]releasedomain.ProviderFreshness{"musicbrainz": {State: "fresh", LastSuccessAt: record.ProviderRecord.ObservedAt, LastObservationID: record.ProviderRecord.PrimaryObservationID}}}
	external := []releasedomain.ExternalID{{Provider: "musicbrainz", Namespace: "recording", Value: record.Recording.ProviderID, Evidence: "provider_record"}}
	for _, isrc := range recording.ISRCs {
		external = append(external, releasedomain.ExternalID{Provider: "isrc", Namespace: "recording", Value: isrc, Evidence: "provider_assertion"})
	}
	doc := releasedomain.RecordingDocument{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: "recording", Slug: slug, Display: releasedomain.Display{Title: recording.Title}, ExternalIDs: external, Data: recording, Freshness: fresh, Provenance: map[string][]releasedomain.SourceRef{"identity": {{Provider: "musicbrainz", ObservationID: record.ProviderRecord.PrimaryObservationID}}, "data": {{Provider: "musicbrainz", ObservationID: record.ProviderRecord.PrimaryObservationID}}}}
	docBody, _ := json.Marshal(doc)
	sum := sha256.Sum256(normalizedBody)
	if _, err := tx.Exec(ctx, `INSERT INTO canonical_recordings(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, releasedomain.RecordingMergeVersion, hex.EncodeToString(sum[:]), docBody); err != nil {
		return Result{}, err
	}
	for _, kind := range []string{"detail", "summary"} {
		if _, err := tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,$2,1,$3,$4,$5)ON CONFLICT(entity_id,document_kind)DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, kind, version, docBody, fresh.FreshUntil); err != nil {
			return Result{}, err
		}
	}
	genres := make([]string, 0, len(recording.Genres))
	for _, genre := range recording.Genres {
		genres = append(genres, genre.Name)
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "recording", "slug": slug, "display": doc.Display, "freshness": fresh})
	if _, err := tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,status,genres,countries,languages,summary,projection_version)VALUES($1,'recording',$2,$3,'',$4,'{}','{}',$5,$6)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,genres=EXCLUDED.genres,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, recording.Title, genres, summary, version); err != nil {
		return Result{}, err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID)
	_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),'display',90)ON CONFLICT DO NOTHING`, entityID, recording.Title)
	_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at)VALUES($1,'musicbrainz',now(),now(),$2,NULLIF($3,0),$4)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at`, entityID, record.ProviderRecord.PrimaryObservationID, jobID, fresh.FreshUntil)
	_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,next_eligible_at)VALUES($1,'lrclib',now())ON CONFLICT(entity_id,provider)DO NOTHING`, entityID)
	change := "updated"
	if created {
		change = "created"
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id)VALUES($1,'recording',$2,$3,$4,$5,$6,NULLIF($7,0))`, entityID, slug, change, []string{"identity", "detail", "search"}, version, record.ProviderRecord.PrimaryObservationID, jobID)
	if jobID > 0 {
		_, _ = tx.Exec(ctx, `UPDATE recording_ingestion_runs SET entity_id=$2,state='completed',completed_at=now(),error=NULL WHERE river_job_id=$1`, jobID, entityID)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{EntityID: entityID, NormalizedID: normalizedID, ProjectionVersion: version, Detail: doc}, nil
}

func persistClaims(ctx context.Context, tx pgx.Tx, entityID string, record releasedomain.NormalizedRecording) error {
	_, err := tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'recording','musicbrainz','recording',$2,'accepted',1,$3,$4,$4)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, strings.ToLower(record.Recording.ProviderID), record.ProviderRecord.PrimaryObservationID, record.ProviderRecord.ObservedAt)
	if err != nil {
		return err
	}
	for _, isrc := range record.Recording.ISRCs {
		value := strings.ToUpper(isrc)
		var existing string
		lookupErr := tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording' AND provider='isrc' AND namespace='recording' AND normalized_value=$1`, value).Scan(&existing)
		if lookupErr == nil && existing != entityID {
			claims, _ := json.Marshal([]map[string]string{{"entity_id": existing, "provider": "isrc", "namespace": "recording", "value": value}, {"entity_id": entityID, "provider": "isrc", "namespace": "recording", "value": value}})
			_, _ = tx.Exec(ctx, `INSERT INTO external_id_conflicts(entity_kind,claims,state)VALUES('recording',$1,'open')`, claims)
			continue
		}
		if lookupErr != nil && lookupErr != pgx.ErrNoRows {
			return lookupErr
		}
		_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'recording','isrc','recording',$2,'proposed',0.95,$3,$4,$4)ON CONFLICT DO NOTHING`, entityID, value, record.ProviderRecord.PrimaryObservationID, record.ProviderRecord.ObservedAt)
	}
	return nil
}

func MergeData(existing, incoming releasedomain.Recording) releasedomain.Recording {
	result := incoming
	if result.Title == "" {
		result.Title = existing.Title
	}
	if result.DurationMS == 0 {
		result.DurationMS = existing.DurationMS
	}
	if result.Disambiguation == "" {
		result.Disambiguation = existing.Disambiguation
	}
	if len(result.ISRCs) == 0 {
		result.ISRCs = existing.ISRCs
	}
	if len(result.ArtistCredits) == 0 {
		result.ArtistCredits = existing.ArtistCredits
	}
	if len(result.Genres) == 0 {
		result.Genres = existing.Genres
	}
	if len(result.Tags) == 0 {
		result.Tags = existing.Tags
	}
	if result.Rating == nil {
		result.Rating = existing.Rating
	}
	if len(result.Releases) == 0 {
		result.Releases = existing.Releases
	}
	if len(result.Links) == 0 {
		result.Links = existing.Links
	}
	return result
}

func resolveOrCreate(ctx context.Context, tx pgx.Tx, mbid, title string) (id, slug string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording' AND provider='musicbrainz' AND namespace='recording' AND normalized_value=$1 AND state='accepted'`, strings.ToLower(mbid)).Scan(&id)
	if err != nil && err != pgx.ErrNoRows {
		return
	}
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1 FOR UPDATE`, id).Scan(&slug)
		return
	}
	created = true
	base := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if base == "" {
		base = "recording"
	}
	for n := 0; ; n++ {
		slug = base
		if n > 0 {
			slug += "-" + strconv.Itoa(n+1)
		}
		err = tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('recording',$1)ON CONFLICT DO NOTHING RETURNING id`, slug).Scan(&id)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,'recording',$2)`, id, slug)
			return
		}
		if err != pgx.ErrNoRows {
			return
		}
	}
}

func (s *Service) Detail(ctx context.Context, id string) (releasedomain.RecordingDocument, bool, error) {
	var body []byte
	var fresh time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, id).Scan(&body, &fresh); err == pgx.ErrNoRows {
		return releasedomain.RecordingDocument{}, false, ErrNotFound
	} else if err != nil {
		return releasedomain.RecordingDocument{}, false, err
	}
	var doc releasedomain.RecordingDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return doc, false, err
	}
	return doc, time.Now().Before(fresh), nil
}

func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	var id string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, strings.ToLower(provider), strings.ToLower(namespace), strings.ToLower(strings.TrimSpace(value))).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return id, err
}

func (s *Service) MusicBrainzID(ctx context.Context, entityID string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='recording' AND provider='musicbrainz' AND namespace='recording' AND state='accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return value, err
}
