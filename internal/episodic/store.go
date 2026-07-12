package episodic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

type Definition struct{ Kind, Provider, Namespace, NormalizerVersion, MergeVersion string }
type Result struct {
	EntityID          string
	ProjectionVersion int64
	Document          Document
}

func StartRun(ctx context.Context, runtime *platform.Runtime, jobID int64, def Definition, id string) error {
	if jobID == 0 {
		return nil
	}
	_, err := runtime.DB.Exec(ctx, `INSERT INTO episodic_ingestion_runs(river_job_id,entity_kind,provider,namespace,provider_id,state) VALUES($1,$2,$3,$4,$5,'working') ON CONFLICT(river_job_id) DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, def.Kind, def.Provider, def.Namespace, id)
	return err
}
func FailRun(ctx context.Context, runtime *platform.Runtime, jobID int64, err error) {
	if jobID > 0 {
		_, _ = runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE episodic_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, err.Error())
	}
}

func Persist(ctx context.Context, runtime *platform.Runtime, def Definition, record NormalizedRecord, jobID int64) (Result, error) {
	return PersistMany(ctx, runtime, def, []NormalizedRecord{record}, jobID)
}
func PersistMany(ctx context.Context, runtime *platform.Runtime, def Definition, records []NormalizedRecord, jobID int64) (Result, error) {
	if len(records) == 0 {
		return Result{}, fmt.Errorf("episodic persistence requires at least one normalized record")
	}
	record := Merge(records)
	body, err := json.Marshal(record)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(sum[:])
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	normalizedIDs := []string{}
	for _, source := range records {
		sourceBody, _ := json.Marshal(source)
		version := source.NormalizerVersion
		if version == "" {
			version = def.NormalizerVersion
		}
		var normalizedID string
		err = tx.QueryRow(ctx, `INSERT INTO normalized_records(entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(primary_observation_id,normalizer_version,schema_version) DO UPDATE SET document=EXCLUDED.document RETURNING id`, def.Kind, source.Provider, source.Namespace, source.ProviderID, source.PrimaryObservationID, version, source.SchemaVersion, sourceBody, source.ObservedAt).Scan(&normalizedID)
		if err != nil {
			return Result{}, err
		}
		normalizedIDs = append(normalizedIDs, normalizedID)
	}
	var entityID, slug string
	err = tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, def.Kind, def.Provider, def.Namespace, record.ProviderID).Scan(&entityID)
	created := false
	if err == pgx.ErrNoRows {
		created = true
		base := Slug(preferredTitle(record.Titles), year(record.StartDate), def.Kind)
		for n := 0; ; n++ {
			slug = base
			if n > 0 {
				slug = fmt.Sprintf("%s-%d", base, n+1)
			}
			e := tx.QueryRow(ctx, `INSERT INTO entities(kind,slug) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING id`, def.Kind, slug).Scan(&entityID)
			if e == nil {
				_, e = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug) VALUES($1,$2,$3)`, entityID, def.Kind, slug)
				if e != nil {
					return Result{}, e
				}
				break
			}
			if e != pgx.ErrNoRows {
				return Result{}, e
			}
		}
	} else if err != nil {
		return Result{}, err
	}
	if slug == "" {
		if err := tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1 FOR UPDATE`, entityID).Scan(&slug); err != nil {
			return Result{}, err
		}
	}
	for _, source := range records {
		// Claims from an older normalization of this provider that disappeared
		// are no longer resolvable identity. This matters for upstream documents
		// that replace an ambiguous related-ID list with one authoritative map.
		_, _ = tx.Exec(ctx, `UPDATE external_id_claims c SET state='superseded',last_observed_at=$3 WHERE c.entity_id=$1 AND c.entity_kind=$2 AND c.state='accepted' AND EXISTS (SELECT 1 FROM provider_observations o WHERE o.id=c.source_observation_id AND o.provider=$4)`, entityID, def.Kind, source.ObservedAt, source.Provider)
		claims := append([]ExternalID(nil), source.ExternalIDs...)
		claims = append(claims, ExternalID{Provider: source.Provider, Namespace: source.Namespace, Value: source.ProviderID})
		for _, external := range claims {
			if external.Value == "" {
				continue
			}
			_, _ = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at) VALUES($1,$2,$3,$4,$5,'accepted',1,$6,$7,$7) ON CONFLICT(entity_kind,provider,namespace,normalized_value) DO UPDATE SET state='accepted',last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, def.Kind, external.Provider, external.Namespace, strings.ToLower(external.Value), source.PrimaryObservationID, source.ObservedAt)
		}
	}
	for _, normalizedID := range normalizedIDs {
		if _, err = tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE id=$2`, entityID, normalizedID); err != nil {
			return Result{}, err
		}
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	freshFor := 14 * 24 * time.Hour
	if !ended(record.Status, record.EndDate) {
		freshFor = 48 * time.Hour
	}
	fresh := Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(freshFor), Providers: map[string]ProviderFreshness{}}
	for _, source := range records {
		fresh.Providers[source.Provider] = ProviderFreshness{State: "fresh", LastSuccessAt: source.ObservedAt, LastObservationID: source.PrimaryObservationID}
	}
	display := Display{Title: preferredTitle(record.Titles), OriginalTitle: originalTitle(record.Titles), Year: year(record.StartDate)}
	publicImages := make([]Image, 0, len(record.Images))
	for _, image := range record.Images {
		if image.URL == "" {
			continue
		}
		var imageID string
		provider := image.Provider
		if provider == "" {
			provider = def.Provider
		}
		observationID := record.PrimaryObservationID
		for _, source := range records {
			if source.Provider == provider {
				observationID = source.PrimaryObservationID
				break
			}
		}
		err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,width,height,source_observation_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8) ON CONFLICT(entity_id,provider,provider_image_id,class) DO UPDATE SET source_url=EXCLUDED.source_url,width=EXCLUDED.width,height=EXCLUDED.height,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, provider, image.ProviderID, image.Class, image.URL, image.Width, image.Height, observationID).Scan(&imageID)
		if err != nil {
			return Result{}, err
		}
		publicImages = append(publicImages, Image{ID: imageID, Provider: provider, ProviderID: image.ProviderID, Class: image.Class, Width: image.Width, Height: image.Height})
		if display.ImageID == "" && image.Class == "poster" {
			display.ImageID = imageID
		}
	}
	genres := record.Genres
	if genres == nil {
		genres = []string{}
	}
	countries := record.Countries
	if countries == nil {
		countries = []string{}
	}
	languages := []string{}
	if record.Language != "" {
		languages = append(languages, record.Language)
	}
	refs := []SourceRef{}
	for _, source := range records {
		refs = append(refs, SourceRef{Provider: source.Provider, ObservationID: source.PrimaryObservationID})
	}
	doc := Document{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: def.Kind, Slug: slug, Display: display, ExternalIDs: record.ExternalIDs, Data: Data{Titles: record.Titles, Overview: record.Overview, Classification: Classification{Format: record.Format, Status: record.Status, Language: record.Language, Countries: countries, Genres: genres, SourceMaterial: record.SourceMaterial}, Lifecycle: Lifecycle{StartDate: record.StartDate, EndDate: record.EndDate}, RuntimeMinutes: record.RuntimeMinutes, EpisodeCount: record.EpisodeCount, Networks: record.Networks, Studios: record.Studios, Seasons: record.Seasons, Episodes: record.Episodes, Images: publicImages}, Freshness: fresh, Provenance: map[string][]SourceRef{"identity": refs, "data": refs}}
	docJSON, _ := json.Marshal(doc)
	table := "canonical_tv_shows"
	if def.Kind == "anime" {
		table = "canonical_anime"
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+table+`(entity_id,merge_version,source_fingerprint,document) VALUES($1,$2,$3,$4) ON CONFLICT(entity_id) DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, def.MergeVersion, fingerprint, docJSON)
	if err != nil {
		return Result{}, err
	}
	summary := Summary{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: def.Kind, Slug: slug, Display: display, Status: record.Status, Genres: genres, Countries: countries, Freshness: fresh}
	summaryJSON, _ := json.Marshal(summary)
	for kind, value := range map[string][]byte{"detail": docJSON, "summary": summaryJSON} {
		_, err = tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until) VALUES($1,$2,1,$3,$4,$5) ON CONFLICT(entity_id,document_kind) DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, kind, version, value, fresh.FreshUntil)
		if err != nil {
			return Result{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,release_year,status,genres,countries,languages,summary,projection_version) VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9,$10,$11) ON CONFLICT(entity_id) DO UPDATE SET slug=EXCLUDED.slug,display_title=EXCLUDED.display_title,release_year=EXCLUDED.release_year,status=EXCLUDED.status,genres=EXCLUDED.genres,countries=EXCLUDED.countries,languages=EXCLUDED.languages,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, def.Kind, slug, display.Title, display.Year, record.Status, genres, countries, languages, summaryJSON, version)
	if err != nil {
		return Result{}, err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID)
	for _, title := range record.Titles {
		normalized := normalize(title.Value)
		if normalized != "" {
			_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,locale,name_type,source_quality) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, entityID, title.Value, normalized, title.Language, title.Type, titleQuality(title.Type))
		}
	}
	for _, source := range records {
		_, _ = tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,next_eligible_at) VALUES($1,$2,$3,$3,$4,$5) ON CONFLICT(entity_id,provider) DO UPDATE SET last_attempt_at=EXCLUDED.last_attempt_at,last_success_at=EXCLUDED.last_success_at,last_observation_id=EXCLUDED.last_observation_id,next_eligible_at=EXCLUDED.next_eligible_at,failure_class=NULL,failure_message=NULL`, entityID, source.Provider, source.ObservedAt, source.PrimaryObservationID, fresh.FreshUntil)
	}
	changeType := "updated"
	if created {
		changeType = "created"
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, entityID, def.Kind, slug, changeType, []string{"identity", "detail", "search", "provenance"}, version, record.PrimaryObservationID, nullableJob(jobID))
	if jobID > 0 {
		_, err = tx.Exec(ctx, `UPDATE episodic_ingestion_runs SET state='completed',entity_id=$2,error=NULL,completed_at=now() WHERE river_job_id=$1`, jobID, entityID)
		if err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	_ = changelog.Sequence(ctx, runtime, 100)
	return Result{EntityID: entityID, ProjectionVersion: version, Document: doc}, nil
}

func Resolve(ctx context.Context, runtime *platform.Runtime, kind, provider, namespace, value string) (string, error) {
	var id string
	err := runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, kind, strings.ToLower(provider), strings.ToLower(namespace), strings.ToLower(strings.TrimSpace(value))).Scan(&id)
	return id, err
}
func Detail(ctx context.Context, runtime *platform.Runtime, kind, id string) (Document, bool, error) {
	var body []byte
	var freshUntil time.Time
	err := runtime.DB.QueryRow(ctx, `SELECT d.document,d.fresh_until FROM api_documents d JOIN entities e ON e.id=d.entity_id WHERE d.entity_id=$1 AND d.document_kind='detail' AND e.kind=$2 AND e.deleted_at IS NULL`, id, kind).Scan(&body, &freshUntil)
	if err != nil {
		return Document{}, false, err
	}
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return Document{}, false, err
	}
	_ = accessstats.Track(ctx, runtime.Redis, id)
	return doc, time.Now().Before(freshUntil), nil
}
func Slug(title string, year int, kind string) string {
	value := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if value == "" {
		value = kind
	}
	if year > 0 {
		value += "-" + strconv.Itoa(year)
	}
	return value
}
func preferredTitle(values []Title) string {
	for _, kind := range []string{"main", "display", "official", "original"} {
		for _, v := range values {
			if v.Type == kind && v.Value != "" {
				return v.Value
			}
		}
	}
	if len(values) > 0 {
		return values[0].Value
	}
	return "untitled"
}
func originalTitle(values []Title) string {
	for _, v := range values {
		if v.Type == "original" || v.Language == "ja" {
			return v.Value
		}
	}
	return ""
}
func year(value string) int {
	if len(value) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(value[:4])
	return n
}
func normalize(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), " ")) }
func titleQuality(kind string) int {
	switch kind {
	case "main", "display":
		return 100
	case "official", "original":
		return 90
	case "alias", "synonym":
		return 70
	}
	return 50
}
func ended(status, end string) bool {
	s := strings.ToLower(status)
	return end != "" || strings.Contains(s, "ended") || strings.Contains(s, "finished")
}
func nullableJob(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
func SortStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		k := strings.ToLower(v)
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
