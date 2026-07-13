package musicalworks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openopus"
	"github.com/jackc/pgx/v5"
)

const normalizerVersion = "openopus-musical-work/v1"

var (
	ErrNotFound         = errors.New("musical work not found")
	ErrProviderNotFound = errors.New("Open Opus musical work not found")
	nonSlug             = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

type Result struct {
	EntityID          string
	NormalizedID      string
	ProjectionVersion int64
	Detail            Document
}

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) IngestOpenOpus(ctx context.Context, workID string, jobID int64) (result Result, returnErr error) {
	workID = strings.TrimSpace(workID)
	if id, err := strconv.ParseInt(workID, 10, 64); err != nil || id < 1 {
		return result, fmt.Errorf("invalid Open Opus work ID")
	}
	if jobID > 0 {
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO musical_work_ingestion_runs(river_job_id,openopus_work_id,state)VALUES($1,$2,'working')ON CONFLICT(river_job_id)DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, workID); err != nil {
			return result, err
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE musical_work_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}

	base := openopus.New(s.runtime.Config.Providers.OpenOpus)
	resolver, err := providercache.New(s.runtime, normalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := openopus.NewCached(s.runtime.Config.Providers.OpenOpus, resolver).Collect(ctx, providers.Identifier{Provider: "openopus", Namespace: "work", Value: workID})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, ErrProviderNotFound
	}
	payload := payloads[0]
	if payload.StatusCode != 200 {
		return result, &providers.StatusError{Provider: "openopus", StatusCode: payload.StatusCode}
	}
	data, response, err := normalizeOpenOpusWork(workID, payload.Body)
	if err != nil {
		return result, err
	}
	result, err = s.persist(ctx, workID, data, response, payload, jobID)
	if err != nil {
		return result, err
	}
	if err := changelog.Sequence(ctx, s.runtime, 100); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) persist(ctx context.Context, workID string, data Data, normalized openOpusResponse, payload providers.Payload, jobID int64) (Result, error) {
	tx, err := s.runtime.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)

	normalizedBody, _ := json.Marshal(normalized)
	var normalizedID string
	if err := tx.QueryRow(ctx, `INSERT INTO normalized_records(entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES('musical_work','openopus','work',$1,$2,$3,1,$4,$5)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET provider_record_id=EXCLUDED.provider_record_id,document=EXCLUDED.document,observed_at=EXCLUDED.observed_at RETURNING id`, workID, payload.ObservationID, normalizerVersion, normalizedBody, payload.ObservedAt).Scan(&normalizedID); err != nil {
		return Result{}, err
	}

	entityID, slug, created, err := resolveOrCreate(ctx, tx, workID, data.Title)
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, normalizedID); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'musical_work','openopus','work',$2,'accepted',1,$3,$4,$4)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, workID, payload.ObservationID, payload.ObservedAt); err != nil {
		return Result{}, err
	}

	if data.Composer.ProviderPersonID != "" {
		_ = tx.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='artist' AND provider='openopus' AND namespace='composer' AND normalized_value=$1 AND state='accepted'`, data.Composer.ProviderPersonID).Scan(&data.Composer.ArtistEntityID)
		metadata, _ := json.Marshal(map[string]any{"name": data.Composer.Name, "epoch": data.Composer.Epoch})
		if _, err := tx.Exec(ctx, `INSERT INTO entity_relations(source_entity_id,target_entity_id,source_kind,target_kind,relation_type,provider,namespace,provider_value,metadata,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,NULLIF($2,'')::uuid,'musical_work','artist','composer','openopus','composer',$3,$4,'accepted',$5,$6,$6)ON CONFLICT(source_entity_id,relation_type,provider,namespace,provider_value)DO UPDATE SET target_entity_id=EXCLUDED.target_entity_id,metadata=EXCLUDED.metadata,state='accepted',source_observation_id=EXCLUDED.source_observation_id,last_observed_at=EXCLUDED.last_observed_at`, entityID, data.Composer.ArtistEntityID, data.Composer.ProviderPersonID, metadata, payload.ObservationID, payload.ObservedAt); err != nil {
			return Result{}, err
		}
	}

	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	freshUntil := now.Add(30 * 24 * time.Hour)
	freshness := Freshness{State: "fresh", UpdatedAt: now, FreshUntil: freshUntil, Providers: map[string]ProviderFreshness{"openopus": {State: "fresh", LastSuccessAt: payload.ObservedAt, LastObservationID: payload.ObservationID}}}
	document := Document{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: Kind, Slug: slug, ExternalIDs: []ExternalID{{Provider: "openopus", Namespace: "work", Value: workID}}, Data: data, Freshness: freshness, Provenance: map[string][]SourceRef{"identity": {{Provider: "openopus", ObservationID: payload.ObservationID}}, "data": {{Provider: "openopus", ObservationID: payload.ObservationID}}, "composer": {{Provider: "openopus", ObservationID: payload.ObservationID}}}}
	document.Display.Title = data.Title
	document.Display.Subtitle = data.Composer.Name
	documentBody, _ := json.Marshal(document)
	sum := sha256.Sum256(normalizedBody)
	if _, err := tx.Exec(ctx, `INSERT INTO canonical_musical_works(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, MergeVersion, hex.EncodeToString(sum[:]), documentBody); err != nil {
		return Result{}, err
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": Kind, "slug": slug, "display": document.Display, "genre": data.Genre, "composer": data.Composer, "freshness": freshness})
	for documentKind, body := range map[string][]byte{"detail": documentBody, "summary": summary} {
		if _, err := tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,$2,1,$3,$4,$5)ON CONFLICT(entity_id,document_kind)DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, documentKind, version, body, freshUntil); err != nil {
			return Result{}, err
		}
	}
	genres := []string{}
	if data.Genre != "" {
		genres = append(genres, data.Genre)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,status,genres,countries,languages,summary,projection_version)VALUES($1,'musical_work',$2,$3,'',$4,'{}','{}',$5,$6)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,genres=EXCLUDED.genres,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, data.Title, genres, summary, version); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID); err != nil {
		return Result{}, err
	}
	names := []struct {
		value, kind string
		quality     int
	}{{data.Title, "display", 100}, {data.Subtitle, "subtitle", 85}, {data.Composer.Name, "composer", 70}}
	for _, term := range data.SearchTerms {
		names = append(names, struct {
			value, kind string
			quality     int
		}{term, "search_term", 75})
	}
	for _, name := range names {
		if strings.TrimSpace(name.value) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),$3,$4)ON CONFLICT DO NOTHING`, entityID, name.value, name.kind, name.quality); err != nil {
			return Result{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at)VALUES($1,'openopus',now(),now(),$2,NULLIF($3,0),$4)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at`, entityID, payload.ObservationID, jobID, freshUntil); err != nil {
		return Result{}, err
	}
	changeType := "updated"
	if created {
		changeType = "created"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id)VALUES($1,'musical_work',$2,$3,$4,$5,$6,NULLIF($7,0))`, entityID, slug, changeType, []string{"identity", "detail", "search", "relations"}, version, payload.ObservationID, jobID); err != nil {
		return Result{}, err
	}
	if jobID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE musical_work_ingestion_runs SET entity_id=$2,state='completed',completed_at=now(),error=NULL WHERE river_job_id=$1`, jobID, entityID); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{EntityID: entityID, NormalizedID: normalizedID, ProjectionVersion: version, Detail: document}, nil
}

func (s *Service) Detail(ctx context.Context, entityID string) (Document, bool, error) {
	var body []byte
	var freshUntil time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, entityID).Scan(&body, &freshUntil); err == pgx.ErrNoRows {
		return Document{}, false, ErrNotFound
	} else if err != nil {
		return Document{}, false, err
	}
	var document Document
	if err := json.Unmarshal(body, &document); err != nil {
		return Document{}, false, err
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
	return document, time.Now().Before(freshUntil), nil
}

func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	var entityID string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='musical_work' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, strings.ToLower(strings.TrimSpace(provider)), strings.ToLower(strings.TrimSpace(namespace)), strings.TrimSpace(value)).Scan(&entityID)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return entityID, err
}

func (s *Service) OpenOpusID(ctx context.Context, entityID string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='musical_work' AND provider='openopus' AND namespace='work' AND state='accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	return value, err
}

func resolveOrCreate(ctx context.Context, tx pgx.Tx, workID, title string) (entityID, slug string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='musical_work' AND provider='openopus' AND namespace='work' AND normalized_value=$1 AND state='accepted'`, workID).Scan(&entityID)
	if err != nil && err != pgx.ErrNoRows {
		return
	}
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, entityID).Scan(&slug)
		return
	}
	created = true
	base := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if base == "" {
		base = "musical-work"
	}
	for suffix := 0; ; suffix++ {
		slug = base
		if suffix > 0 {
			slug += "-" + strconv.Itoa(suffix+1)
		}
		err = tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('musical_work',$1)ON CONFLICT DO NOTHING RETURNING id::text`, slug).Scan(&entityID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,'musical_work',$2)`, entityID, slug)
			return
		}
		if err != pgx.ErrNoRows {
			return
		}
	}
}

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if key != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeOpenOpusWork(workID string, body []byte) (Data, openOpusResponse, error) {
	var response openOpusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Data{}, response, fmt.Errorf("decode Open Opus work: %w", err)
	}
	if !strings.EqualFold(response.Status.Success, "true") || response.Work.ID == "" || response.Work.Title == "" {
		return Data{}, response, ErrProviderNotFound
	}
	if response.Work.ID != workID {
		return Data{}, response, fmt.Errorf("Open Opus returned work %s for requested work %s", response.Work.ID, workID)
	}
	data := Data{
		Title:       strings.TrimSpace(response.Work.Title),
		Subtitle:    strings.TrimSpace(response.Work.Subtitle),
		Genre:       strings.TrimSpace(response.Work.Genre),
		SearchTerms: cleanStrings(response.Work.SearchTerms),
		Catalogue: Catalogue{
			System:           strings.TrimSpace(response.Work.Catalogue),
			Number:           strings.TrimSpace(response.Work.CatalogueNumber),
			AdditionalNumber: strings.TrimSpace(response.Work.AdditionalNumber),
		},
		Composer: Composer{
			Name:             firstNonEmpty(response.Composer.CompleteName, response.Composer.Name),
			Provider:         "openopus",
			ProviderPersonID: strings.TrimSpace(response.Composer.ID),
			Epoch:            strings.TrimSpace(response.Composer.Epoch),
		},
	}
	return data, response, nil
}
