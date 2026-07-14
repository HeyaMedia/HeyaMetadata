// Package releases ingests canonical issued editions and materializes their
// media, release-track placements, and referenced canonical recordings.
package releases

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

	"github.com/HeyaMedia/HeyaMetadata/internal/canonicalrefs"
	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprint"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = fmt.Errorf("release not found")
var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

type Result struct {
	EntityID, NormalizedID string
	ProjectionVersion      int64
	Detail                 releasedomain.DetailDocument
}
type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) IngestMusicBrainz(ctx context.Context, mbid string, jobID int64) (result Result, returnErr error) {
	return s.IngestMusicBrainzWithCredentials(ctx, mbid, jobID, providercredentials.Credentials{})
}
func (s *Service) IngestMusicBrainzWithCredentials(ctx context.Context, mbid string, jobID int64, credentials providercredentials.Credentials) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if jobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO release_ingestion_runs(river_job_id,musicbrainz_id,state)VALUES($1,$2,'working')ON CONFLICT(river_job_id)DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, mbid); err != nil {
			return result, err
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE release_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, releasedomain.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver).Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "release", Value: mbid})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("MusicBrainz returned no release")
	}
	p := payloads[0]
	if p.StatusCode != 200 {
		return result, &providers.StatusError{Provider: "musicbrainz", StatusCode: p.StatusCode}
	}
	record, err := musicbrainz.NormalizeRelease(p.Body, p.ObservationID, p.ObservedAt)
	if err != nil {
		return result, err
	}
	records := append([]releasedomain.NormalizedRecord{record}, s.collectSupplements(ctx, record, jobID, credentials)...)
	evidence := s.collectRecordingEvidence(ctx, records)
	result, err = s.persist(ctx, records, evidence, jobID)
	if err != nil {
		return result, err
	}
	if err = s.cache(ctx, result); err != nil {
		return result, err
	}
	if err = changelog.Sequence(ctx, s.runtime, 100); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) persist(ctx context.Context, records []releasedomain.NormalizedRecord, evidence evidenceBundle, jobID int64) (Result, error) {
	if len(records) == 0 {
		return Result{}, fmt.Errorf("release persistence requires a MusicBrainz spine")
	}
	r := records[0]
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	body, _ := json.Marshal(records)
	normalizedIDs := []string{}
	for _, source := range records {
		sourceBody, _ := json.Marshal(source)
		var id string
		if err = tx.QueryRow(ctx, `INSERT INTO normalized_records(entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES('release',$1,$2,$3,$4,$5,$6,$7,$8)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET document=EXCLUDED.document RETURNING id`, source.ProviderRecord.Provider, source.ProviderRecord.Namespace, source.ProviderRecord.Value, source.ProviderRecord.PrimaryObservationID, source.ProviderRecord.NormalizerVersion, source.ProviderRecord.SchemaVersion, sourceBody, source.ProviderRecord.ObservedAt).Scan(&id); err != nil {
			return Result{}, err
		}
		normalizedIDs = append(normalizedIDs, id)
	}
	normalizedID := normalizedIDs[0]
	entityID, slug, created, err := resolveOrCreate(ctx, tx, "release", "musicbrainz", "release", r.ProviderRecord.Value, r.Title, year(r.Date))
	if err != nil {
		return Result{}, err
	}
	for _, id := range normalizedIDs {
		if _, err = tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, id); err != nil {
			return Result{}, err
		}
	}
	for _, source := range records {
		for _, id := range source.ExternalIDs {
			_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'release',$2,$3,$4,'accepted',1,$5,$6,$6)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET state='accepted',last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, id.Provider, id.Namespace, strings.ToLower(id.Value), source.ProviderRecord.PrimaryObservationID, source.ProviderRecord.ObservedAt)
		}
	}
	var version int64
	if err = tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	fresh := releasedomain.Freshness{State: "fresh", UpdatedAt: time.Now().UTC(), FreshUntil: time.Now().UTC().Add(7 * 24 * time.Hour), Providers: map[string]releasedomain.ProviderFreshness{}}
	refs := []releasedomain.SourceRef{}
	external := []releasedomain.ExternalID{}
	sources := []releasedomain.EditionSource{}
	for _, source := range records {
		fresh.Providers[source.ProviderRecord.Provider] = releasedomain.ProviderFreshness{State: "fresh", LastSuccessAt: source.ProviderRecord.ObservedAt, LastObservationID: source.ProviderRecord.PrimaryObservationID}
		refs = append(refs, releasedomain.SourceRef{Provider: source.ProviderRecord.Provider, ObservationID: source.ProviderRecord.PrimaryObservationID})
		external = append(external, source.ExternalIDs...)
		sources = append(sources, releasedomain.EditionSource{Provider: source.ProviderRecord.Provider, Namespace: source.ProviderRecord.Namespace, ProviderID: source.ProviderRecord.Value, Title: source.Title, Barcode: source.Barcode, Date: source.Date, Link: source.Link})
	}
	doc := releasedomain.DetailDocument{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: "release", Slug: slug, Display: releasedomain.Display{Title: r.Title, Year: year(r.Date)}, ExternalIDs: external, Data: releasedomain.ReleaseData{Title: r.Title, Disambiguation: r.Disambiguation, Status: r.Status, Quality: r.Quality, Packaging: r.Packaging, Date: r.Date, Country: r.Country, Barcode: r.Barcode, ArtistCredits: r.ArtistCredits, Labels: r.Labels, Sources: sources}, Freshness: fresh, Provenance: map[string][]releasedomain.SourceRef{"identity": {{Provider: "musicbrainz", ObservationID: r.ProviderRecord.PrimaryObservationID}}, "data": refs}}
	_, _ = tx.Exec(ctx, `DELETE FROM release_media WHERE release_entity_id=$1`, entityID)
	for _, medium := range r.Media {
		var mediumID string
		discIDs, _ := json.Marshal(medium.DiscIDs)
		if err = tx.QueryRow(ctx, `INSERT INTO release_media(release_entity_id,position,title,format,track_count,disc_ids)VALUES($1,$2,$3,$4,$5,$6)RETURNING id`, entityID, medium.Position, medium.Title, medium.Format, medium.TrackCount, discIDs).Scan(&mediumID); err != nil {
			return Result{}, err
		}
		projected := releasedomain.MediumDocument{ID: mediumID, Position: medium.Position, Title: medium.Title, Format: medium.Format, TrackCount: medium.TrackCount, DiscIDs: medium.DiscIDs}
		for _, track := range medium.Tracks {
			recordingID, recordingVersion, err := s.persistRecording(ctx, tx, track.Recording, r.ProviderRecord, fresh)
			if err != nil {
				return Result{}, err
			}
			_ = recordingVersion
			if err = recordings.PersistWorkRelations(ctx, tx, recordingID, track.WorkRelations, r.ProviderRecord); err != nil {
				return Result{}, err
			}
			if err = persistRecordingEvidence(ctx, tx, recordingID, track.Recording.ProviderID, evidence); err != nil {
				return Result{}, err
			}
			trackSources := []releasedomain.TrackSource{{Provider: "musicbrainz", Namespace: "track", ProviderID: track.ProviderID}}
			for _, source := range records[1:] {
				if match := releasedomain.MatchTrack(track, source, medium.Position); match != nil {
					isrc := ""
					if len(match.Recording.ISRCs) > 0 {
						isrc = match.Recording.ISRCs[0]
					}
					trackSources = append(trackSources, releasedomain.TrackSource{Provider: source.ProviderRecord.Provider, Namespace: "track", ProviderID: match.ProviderID, ISRC: isrc})
					if recordingID != "" && match.ProviderID != "" {
						_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'recording',$2,'track',lower($3),'accepted',1,$4,$5,$5)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET state='accepted',last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, recordingID, source.ProviderRecord.Provider, match.ProviderID, source.ProviderRecord.PrimaryObservationID, source.ProviderRecord.ObservedAt)
					}
				}
			}
			trackBody, _ := json.Marshal(struct {
				Track   releasedomain.Track         `json:"track"`
				Sources []releasedomain.TrackSource `json:"sources"`
			}{Track: track, Sources: trackSources})
			var trackID string
			if err = tx.QueryRow(ctx, `INSERT INTO release_tracks(release_entity_id,medium_id,sequence,position,number,title,duration_ms,recording_entity_id,provider,provider_track_id,document)VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,0),NULLIF($8,'' )::uuid,'musicbrainz',$9,$10)RETURNING id`, entityID, mediumID, track.Sequence, track.Position, track.Number, track.Title, track.DurationMS, recordingID, track.ProviderID, trackBody).Scan(&trackID); err != nil {
				return Result{}, err
			}
			projected.Tracks = append(projected.Tracks, releasedomain.TrackDocument{ID: trackID, RecordingEntityID: recordingID, ProviderID: track.ProviderID, Position: track.Position, Number: track.Number, Title: track.Title, Sequence: track.Sequence, DurationMS: track.DurationMS, ArtistCredits: track.ArtistCredits, Recording: releasedomain.RecordingRef{ID: recordingID, Provider: track.Recording.Provider, Namespace: track.Recording.Namespace, ProviderID: track.Recording.ProviderID, Title: track.Recording.Title, DurationMS: track.Recording.DurationMS, ISRCs: track.Recording.ISRCs}, Sources: trackSources})
		}
		doc.Data.Media = append(doc.Data.Media, projected)
	}
	if err = hydrateReleaseCanonicalIDs(ctx, tx, &doc); err != nil {
		return Result{}, err
	}
	docJSON, _ := json.Marshal(doc)
	sum := sha256.Sum256(body)
	if _, err = tx.Exec(ctx, `INSERT INTO canonical_releases(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, releasedomain.MergeVersion, hex.EncodeToString(sum[:]), docJSON); err != nil {
		return Result{}, err
	}
	for _, kind := range []string{"detail", "summary"} {
		payload := docJSON
		if _, err = tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,$2,1,$3,$4,$5)ON CONFLICT(entity_id,document_kind)DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, kind, version, payload, fresh.FreshUntil); err != nil {
			return Result{}, err
		}
	}
	genres := []string{}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "release", "slug": slug, "display": doc.Display, "freshness": fresh})
	_, err = tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,release_year,status,genres,countries,languages,summary,projection_version)VALUES($1,'release',$2,$3,NULLIF($4,0),$5,$6,$7,'{}',$8,$9)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,release_year=EXCLUDED.release_year,status=EXCLUDED.status,countries=EXCLUDED.countries,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, r.Title, year(r.Date), r.Status, genres, []string{r.Country}, summary, version)
	if err != nil {
		return Result{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE entity_relations SET target_entity_id=$1 WHERE target_kind='release' AND provider='musicbrainz' AND namespace='release' AND provider_value=$2 AND state='accepted'`, entityID, strings.ToLower(r.ProviderRecord.Value)); err != nil {
		return Result{}, fmt.Errorf("link release relations: %w", err)
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID)
	_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),'display',90)ON CONFLICT DO NOTHING`, entityID, r.Title)
	for _, source := range records {
		_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at)VALUES($1,$2,now(),now(),$3,NULLIF($4,0),$5)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at`, entityID, source.ProviderRecord.Provider, source.ProviderRecord.PrimaryObservationID, jobID, fresh.FreshUntil)
	}
	change := "updated"
	if created {
		change = "created"
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id)VALUES($1,'release',$2,$3,$4,$5,$6,NULLIF($7,0))`, entityID, slug, change, []string{"identity", "detail", "tracks", "recordings", "search"}, version, r.ProviderRecord.PrimaryObservationID, jobID)
	if jobID > 0 {
		_, _ = tx.Exec(ctx, `UPDATE release_ingestion_runs SET entity_id=$2,state='completed',completed_at=now(),error=NULL WHERE river_job_id=$1`, jobID, entityID)
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{EntityID: entityID, NormalizedID: normalizedID, ProjectionVersion: version, Detail: doc}, nil
}

func (s *Service) persistRecording(ctx context.Context, tx pgx.Tx, r releasedomain.Recording, source releasedomain.ProviderRecord, fresh releasedomain.Freshness) (string, int64, error) {
	if r.ProviderID == "" {
		return "", 0, nil
	}
	id, slug, _, err := resolveOrCreate(ctx, tx, "recording", r.Provider, r.Namespace, r.ProviderID, r.Title, 0)
	if err != nil {
		return "", 0, err
	}
	var existing releasedomain.RecordingDocument
	var existingBody []byte
	if loadErr := tx.QueryRow(ctx, `SELECT document FROM canonical_recordings WHERE entity_id=$1`, id).Scan(&existingBody); loadErr == nil {
		_ = json.Unmarshal(existingBody, &existing)
	} else if loadErr != pgx.ErrNoRows {
		return "", 0, loadErr
	}
	r = recordings.MergeData(existing.Data, r)
	if err = hydrateRecordingCanonicalIDs(ctx, tx, &r); err != nil {
		return "", 0, err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'recording',$2,$3,$4,'accepted',1,$5,$6,$6)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET state='accepted',last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, id, r.Provider, r.Namespace, strings.ToLower(r.ProviderID), source.PrimaryObservationID, source.ObservedAt)
	for _, isrc := range r.ISRCs {
		value := strings.ToUpper(isrc)
		var existing string
		lookupErr := tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording' AND provider='isrc' AND namespace='recording' AND normalized_value=$1`, value).Scan(&existing)
		if lookupErr == nil && existing != id {
			claims, _ := json.Marshal([]map[string]string{{"entity_id": existing, "provider": "isrc", "namespace": "recording", "value": value}, {"entity_id": id, "provider": "isrc", "namespace": "recording", "value": value}})
			_, _ = tx.Exec(ctx, `INSERT INTO external_id_conflicts(entity_kind,claims,state)VALUES('recording',$1,'open')`, claims)
			continue
		}
		if lookupErr != nil && lookupErr != pgx.ErrNoRows {
			return "", 0, lookupErr
		}
		_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'recording','isrc','recording',$2,'proposed',0.95,$3,$4,$4)ON CONFLICT DO NOTHING`, id, value, source.PrimaryObservationID, source.ObservedAt)
	}
	var version int64
	if err = tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now()WHERE id=$1 RETURNING canonical_version`, id).Scan(&version); err != nil {
		return "", 0, err
	}
	external := []releasedomain.ExternalID{{Provider: r.Provider, Namespace: r.Namespace, Value: r.ProviderID, Evidence: "provider_record"}}
	for _, isrc := range r.ISRCs {
		external = append(external, releasedomain.ExternalID{Provider: "isrc", Namespace: "recording", Value: isrc, Evidence: "provider_assertion"})
	}
	recordingFresh := fresh
	recordingFresh.Providers = map[string]releasedomain.ProviderFreshness{source.Provider: {State: "fresh", LastSuccessAt: source.ObservedAt, LastObservationID: source.PrimaryObservationID}}
	doc := releasedomain.RecordingDocument{SchemaVersion: 1, ProjectionVersion: version, ID: id, Kind: "recording", Slug: slug, Display: releasedomain.Display{Title: r.Title}, ExternalIDs: external, Data: r, Freshness: recordingFresh, Provenance: map[string][]releasedomain.SourceRef{"data": {{Provider: source.Provider, ObservationID: source.PrimaryObservationID}}}}
	body, _ := json.Marshal(doc)
	sum := sha256.Sum256(body)
	_, err = tx.Exec(ctx, `INSERT INTO canonical_recordings(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, id, releasedomain.RecordingMergeVersion, hex.EncodeToString(sum[:]), body)
	if err != nil {
		return "", 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,'detail',1,$2,$3,$4)ON CONFLICT(entity_id,document_kind)DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, id, version, body, fresh.FreshUntil)
	if err == nil {
		summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": id, "kind": "recording", "slug": slug, "display": doc.Display, "freshness": fresh})
		_, err = tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,status,genres,countries,languages,summary,projection_version)VALUES($1,'recording',$2,$3,'','{}','{}','{}',$4,$5)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, id, slug, r.Title, summary, version)
		_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, id)
		_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),'display',90)ON CONFLICT DO NOTHING`, id, r.Title)
	}
	_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,next_eligible_at)VALUES($1,'musicbrainz',now(),now(),$2,$3)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,next_eligible_at=EXCLUDED.next_eligible_at`, id, source.PrimaryObservationID, fresh.FreshUntil)
	_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,next_eligible_at)VALUES($1,'lrclib',now())ON CONFLICT(entity_id,provider)DO NOTHING`, id)
	return id, version, err
}

func persistRecordingEvidence(ctx context.Context, tx pgx.Tx, recordingID, recordingProviderID string, bundle evidenceBundle) error {
	if recordingID == "" || recordingProviderID == "" {
		return nil
	}
	for _, value := range bundle.Fingerprints {
		if value.RecordingProviderID != recordingProviderID {
			continue
		}
		var fingerprintID string
		err := tx.QueryRow(ctx, `INSERT INTO recording_fingerprints(recording_entity_id,algorithm,algorithm_version,generator_version,source_provider,source_track_id,source_checksum,fingerprint,duration_ms,hash_count,state,failure_class,failure_message,retry_after)VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),$10,$11,NULLIF($12,''),NULLIF($13,''),$14)ON CONFLICT(source_provider,source_track_id,algorithm_version)DO UPDATE SET generator_version=EXCLUDED.generator_version,source_checksum=EXCLUDED.source_checksum,fingerprint=EXCLUDED.fingerprint,duration_ms=EXCLUDED.duration_ms,hash_count=EXCLUDED.hash_count,state=EXCLUDED.state,failure_class=EXCLUDED.failure_class,failure_message=EXCLUDED.failure_message,retry_after=EXCLUDED.retry_after,generated_at=now(),updated_at=now() WHERE recording_fingerprints.recording_entity_id=EXCLUDED.recording_entity_id RETURNING id`, recordingID, fingerprint.Algorithm, fingerprint.AlgorithmVersion, value.GeneratorVersion, value.SourceProvider, value.SourceTrackID, value.SourceChecksum, value.Fingerprint, value.DurationMS, value.HashCount, value.State, value.FailureClass, value.FailureMessage, value.RetryAfter).Scan(&fingerprintID)
		if err != nil {
			return err
		}
		if value.State == "ready" {
			if _, err = tx.Exec(ctx, `DELETE FROM recording_fingerprint_landmarks WHERE fingerprint_id=$1`, fingerprintID); err != nil {
				return err
			}
			for _, token := range fingerprint.LandmarkTokens(value.Fingerprint) {
				if _, err = tx.Exec(ctx, `INSERT INTO recording_fingerprint_landmarks(fingerprint_id,token)VALUES($1,$2)ON CONFLICT DO NOTHING`, fingerprintID, token); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func resolveOrCreate(ctx context.Context, tx pgx.Tx, kind, provider, namespace, value, title string, y int) (id, slug string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, kind, provider, namespace, strings.ToLower(value)).Scan(&id)
	if err != nil && err != pgx.ErrNoRows {
		return
	}
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1 FOR UPDATE`, id).Scan(&slug)
		return
	}
	created = true
	base := slugify(title, y, kind)
	for n := 0; ; n++ {
		slug = base
		if n > 0 {
			slug += fmt.Sprintf("-%d", n+1)
		}
		err = tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES($1,$2)ON CONFLICT DO NOTHING RETURNING id`, kind, slug).Scan(&id)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,$2,$3)`, id, kind, slug)
			return
		}
		if err != pgx.ErrNoRows {
			return
		}
	}
}
func slugify(title string, y int, fallback string) string {
	v := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if v == "" {
		v = fallback
	}
	if y > 0 {
		v += "-" + strconv.Itoa(y)
	}
	return v
}
func year(v string) int {
	if len(v) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(v[:4])
	return n
}
func (s *Service) cache(ctx context.Context, r Result) error {
	body, _ := json.Marshal(r.Detail)
	return s.runtime.Redis.Set(ctx, "heya:metadata:v1:api:entity:"+r.EntityID+":detail", body, time.Until(r.Detail.Freshness.FreshUntil)).Err()
}
func (s *Service) Detail(ctx context.Context, id string) (releasedomain.DetailDocument, bool, error) {
	var body []byte
	var fresh time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, id).Scan(&body, &fresh); err == pgx.ErrNoRows {
		return releasedomain.DetailDocument{}, false, ErrNotFound
	} else if err != nil {
		return releasedomain.DetailDocument{}, false, err
	}
	var d releasedomain.DetailDocument
	if err := json.Unmarshal(body, &d); err != nil {
		return d, false, err
	}
	for mediumIndex := range d.Data.Media {
		for trackIndex := range d.Data.Media[mediumIndex].Tracks {
			track := &d.Data.Media[mediumIndex].Tracks[trackIndex]
			if track.RecordingEntityID == "" {
				track.RecordingEntityID = track.Recording.ID
			}
		}
	}
	if err := hydrateReleaseCanonicalIDs(ctx, s.runtime.DB, &d); err != nil {
		return d, false, err
	}
	return d, time.Now().Before(fresh), nil
}

func hydrateReleaseCanonicalIDs(ctx context.Context, db canonicalrefs.Querier, document *releasedomain.DetailDocument) error {
	credits := make([]*releasedomain.ArtistCredit, 0, len(document.Data.ArtistCredits))
	recordingIDs := make([]string, 0)
	for index := range document.Data.ArtistCredits {
		credits = append(credits, &document.Data.ArtistCredits[index])
	}
	for mediumIndex := range document.Data.Media {
		for trackIndex := range document.Data.Media[mediumIndex].Tracks {
			track := &document.Data.Media[mediumIndex].Tracks[trackIndex]
			track.LyricsAvailable = false
			recordingIDs = append(recordingIDs, track.RecordingEntityID)
			for creditIndex := range track.ArtistCredits {
				credits = append(credits, &track.ArtistCredits[creditIndex])
			}
		}
	}
	if err := hydrateReleaseArtistCredits(ctx, db, credits); err != nil {
		return err
	}
	availability, err := recordings.LyricsAvailability(ctx, db, recordingIDs)
	if err != nil {
		return err
	}
	for mediumIndex := range document.Data.Media {
		for trackIndex := range document.Data.Media[mediumIndex].Tracks {
			track := &document.Data.Media[mediumIndex].Tracks[trackIndex]
			track.LyricsAvailable = availability[track.RecordingEntityID]
		}
	}
	return nil
}

func hydrateRecordingCanonicalIDs(ctx context.Context, db canonicalrefs.Querier, recording *releasedomain.Recording) error {
	credits := make([]*releasedomain.ArtistCredit, 0, len(recording.ArtistCredits))
	for index := range recording.ArtistCredits {
		credits = append(credits, &recording.ArtistCredits[index])
	}
	if err := hydrateReleaseArtistCredits(ctx, db, credits); err != nil {
		return err
	}
	releaseRefs := make([]canonicalrefs.Ref, 0, len(recording.Releases))
	groupRefs := make([]canonicalrefs.Ref, 0, len(recording.Releases))
	for _, release := range recording.Releases {
		releaseRefs = append(releaseRefs, canonicalrefs.Ref{Provider: "musicbrainz", Namespace: "release", Value: release.ProviderID})
		groupRefs = append(groupRefs, canonicalrefs.Ref{Provider: "musicbrainz", Namespace: "release_group", Value: release.ReleaseGroupID})
	}
	releasesByRef, err := canonicalrefs.Resolve(ctx, db, "release", releaseRefs)
	if err != nil {
		return err
	}
	groupsByRef, err := canonicalrefs.Resolve(ctx, db, "release_group", groupRefs)
	if err != nil {
		return err
	}
	for index := range recording.Releases {
		release := &recording.Releases[index]
		releaseRef := canonicalrefs.Ref{Provider: "musicbrainz", Namespace: "release", Value: release.ProviderID}
		groupRef := canonicalrefs.Ref{Provider: "musicbrainz", Namespace: "release_group", Value: release.ReleaseGroupID}
		release.ReleaseEntityID = releasesByRef[canonicalrefs.Key(releaseRef)]
		release.ReleaseResolutionState = "unresolved"
		if release.ReleaseEntityID != "" {
			release.ReleaseResolutionState = "materialized"
		}
		release.ReleaseGroupEntityID = groupsByRef[canonicalrefs.Key(groupRef)]
		release.ReleaseGroupResolutionState = "unresolved"
		if release.ReleaseGroupEntityID != "" {
			release.ReleaseGroupResolutionState = "materialized"
		}
	}
	return nil
}

func hydrateReleaseArtistCredits(ctx context.Context, db canonicalrefs.Querier, credits []*releasedomain.ArtistCredit) error {
	refs := make([]canonicalrefs.Ref, 0, len(credits))
	for _, credit := range credits {
		refs = append(refs, canonicalrefs.Ref{Provider: credit.ArtistProvider, Namespace: credit.ArtistNamespace, Value: credit.ArtistID})
	}
	resolved, err := canonicalrefs.Resolve(ctx, db, "artist", refs)
	if err != nil {
		return err
	}
	for _, credit := range credits {
		ref := canonicalrefs.Ref{Provider: credit.ArtistProvider, Namespace: credit.ArtistNamespace, Value: credit.ArtistID}
		credit.ArtistEntityID = resolved[canonicalrefs.Key(ref)]
		credit.ResolutionState = "unresolved"
		if credit.ArtistEntityID != "" {
			credit.ResolutionState = "materialized"
		}
	}
	return nil
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	var id string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='release' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, strings.ToLower(provider), strings.ToLower(namespace), strings.ToLower(strings.TrimSpace(value))).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return id, err
}
func (s *Service) MusicBrainzID(ctx context.Context, entityID string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='release' AND provider='musicbrainz' AND namespace='release' AND state='accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return value, err
}
