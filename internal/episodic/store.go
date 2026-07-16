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
	"github.com/HeyaMedia/HeyaMetadata/internal/people"
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
	// A provider-root promotion must preserve the canonical Heya identity. Use
	// every accepted identifier carried by the incoming records to converge on
	// an existing entity before allocating a new UUID. Conflicting accepted
	// claims are never silently merged.
	entityIDs := map[string]bool{}
	for _, source := range records {
		claims := append([]ExternalID(nil), source.ExternalIDs...)
		claims = append(claims, ExternalID{Provider: source.Provider, Namespace: source.Namespace, Value: source.ProviderID})
		for _, claim := range claims {
			if strings.TrimSpace(claim.Value) == "" {
				continue
			}
			var claimedEntityID string
			claimErr := tx.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, def.Kind, strings.ToLower(strings.TrimSpace(claim.Provider)), strings.ToLower(strings.TrimSpace(claim.Namespace)), strings.ToLower(strings.TrimSpace(claim.Value))).Scan(&claimedEntityID)
			if claimErr == nil {
				entityIDs[claimedEntityID] = true
			} else if claimErr != pgx.ErrNoRows {
				return Result{}, claimErr
			}
		}
	}
	if len(entityIDs) > 1 {
		values := make([]string, 0, len(entityIDs))
		for id := range entityIDs {
			values = append(values, id)
		}
		sort.Strings(values)
		return Result{}, fmt.Errorf("episodic identity evidence conflicts across canonical entities: %s", strings.Join(values, ", "))
	}
	for id := range entityIDs {
		entityID = id
	}
	created := false
	if entityID == "" {
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
	currentProviders := make(map[string]bool, len(records))
	for _, source := range records {
		currentProviders[source.Provider] = true
	}
	fallbacks, err := storedProviderFallbacks(ctx, tx, entityID, def.Kind, currentProviders)
	if err != nil {
		return Result{}, err
	}
	records = append(records, fallbacks...)
	record = Merge(records)
	body, err := json.Marshal(record)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(sum[:])
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
		state := "stale"
		if currentProviders[source.Provider] {
			state = "fresh"
		}
		fresh.Providers[source.Provider] = ProviderFreshness{State: state, LastSuccessAt: source.ObservedAt, LastObservationID: source.PrimaryObservationID}
	}
	display := Display{Title: preferredTitle(record.Titles), OriginalTitle: originalTitle(record.Titles), Year: year(record.StartDate)}
	// Allocate stable child UUIDs before artwork materialization so season and
	// episode images can carry an opaque owner ID in the same transaction.
	if err := persistResources(ctx, tx, entityID, def.Kind, &record); err != nil {
		return Result{}, err
	}
	observationFor := func(provider string) string {
		for _, source := range records {
			if source.Provider == provider {
				return source.PrimaryObservationID
			}
		}
		return record.PrimaryObservationID
	}
	materializeImage := func(image Image, ownershipScope, ownerResourceID string) (Image, error) {
		if image.URL == "" {
			return image, nil
		}
		provider := image.Provider
		if provider == "" {
			provider = def.Provider
		}
		var imageID string
		err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,language,country,width,height,provider_score,source_observation_id,ownership_scope,owner_resource_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),$11,$12,NULLIF($13,'')::uuid) ON CONFLICT(entity_id,provider,provider_image_id,class) DO UPDATE SET source_url=EXCLUDED.source_url,language=EXCLUDED.language,country=EXCLUDED.country,width=EXCLUDED.width,height=EXCLUDED.height,provider_score=EXCLUDED.provider_score,source_observation_id=EXCLUDED.source_observation_id,ownership_scope=EXCLUDED.ownership_scope,owner_resource_id=EXCLUDED.owner_resource_id RETURNING id`, entityID, provider, image.ProviderID, image.Class, image.URL, image.Language, image.Country, image.Width, image.Height, image.ProviderScore, observationFor(provider), ownershipScope, ownerResourceID).Scan(&imageID)
		if err != nil {
			return Image{}, err
		}
		return Image{ID: imageID, Provider: provider, ProviderID: image.ProviderID, Class: image.Class, Language: image.Language, Country: image.Country, Width: image.Width, Height: image.Height, ProviderScore: image.ProviderScore}, nil
	}
	publicImages := make([]Image, 0, len(record.Images))
	for _, image := range record.Images {
		if image.URL == "" {
			continue
		}
		publicImage, err := materializeImage(image, "entity", "")
		if err != nil {
			return Result{}, err
		}
		publicImages = append(publicImages, publicImage)
		if display.ImageID == "" && image.Class == "poster" {
			display.ImageID = publicImage.ID
		}
	}
	for i := range record.Seasons {
		public := make([]Image, 0, len(record.Seasons[i].Images))
		for _, image := range record.Seasons[i].Images {
			materialized, err := materializeImage(image, "season", record.Seasons[i].ID)
			if err != nil {
				return Result{}, err
			}
			if materialized.ID != "" {
				public = append(public, materialized)
			}
		}
		record.Seasons[i].Images = public
	}
	for i := range record.Episodes {
		public := make([]Image, 0, len(record.Episodes[i].Images))
		for _, image := range record.Episodes[i].Images {
			materialized, err := materializeImage(image, "episode", record.Episodes[i].ID)
			if err != nil {
				return Result{}, err
			}
			if materialized.ID != "" {
				public = append(public, materialized)
			}
		}
		record.Episodes[i].Images = public
	}
	for i := range record.Networks {
		item := &record.Networks[i]
		item.ResolutionState = "unresolved"
		if item.EntityID != "" {
			item.ResolutionState = "materialized"
		}
		if item.LogoURL == "" {
			continue
		}
		image, err := materializeImage(Image{Provider: item.LogoProvider, ProviderID: item.LogoProviderID, URL: item.LogoURL, Class: "logo"}, "company", "")
		if err != nil {
			return Result{}, err
		}
		item.LogoImageID = image.ID
		item.LogoURL, item.LogoProvider, item.LogoProviderID = "", "", ""
	}
	for i := range record.Organizations {
		item := &record.Organizations[i]
		item.ResolutionState = "unresolved"
		if item.EntityID != "" {
			item.ResolutionState = "materialized"
		}
		if item.LogoURL == "" {
			continue
		}
		image, err := materializeImage(Image{Provider: item.LogoProvider, ProviderID: item.LogoProviderID, URL: item.LogoURL, Class: "logo"}, "company", "")
		if err != nil {
			return Result{}, err
		}
		item.LogoImageID = image.ID
		item.LogoURL, item.LogoProvider, item.LogoProviderID = "", "", ""
	}
	for i := range record.Recommendations {
		recommendation := &record.Recommendations[i]
		for _, external := range recommendation.ExternalIDs {
			_ = tx.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted' LIMIT 1`, def.Kind, external.Provider, external.Namespace, strings.ToLower(external.Value)).Scan(&recommendation.EntityID)
			if recommendation.EntityID != "" {
				break
			}
		}
		recommendation.ResolutionState = "unresolved"
		if recommendation.EntityID != "" {
			recommendation.ResolutionState = "materialized"
		}
		if recommendation.ImageURL != "" {
			image, err := materializeImage(Image{Provider: recommendation.Provider, ProviderID: "recommendation:" + recommendation.ProviderID, URL: recommendation.ImageURL, Class: "poster", ProviderScore: recommendation.ProviderScore}, "recommendation", "")
			if err != nil {
				return Result{}, err
			}
			recommendation.ImageID = image.ID
		}
	}
	for i := range record.Credits {
		credit := &record.Credits[i]
		if credit.ProfileURL == "" || credit.ProviderPersonID == "" {
			continue
		}
		observationID := record.PrimaryObservationID
		for _, source := range records {
			if source.Provider == credit.Provider {
				observationID = source.PrimaryObservationID
				break
			}
		}
		var imageID string
		if err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,source_observation_id,ownership_scope)VALUES($1,$2,$3,'profile',$4,$5,'credit')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,source_observation_id=EXCLUDED.source_observation_id,ownership_scope='credit' RETURNING id`, entityID, credit.Provider, "person:"+credit.ProviderPersonID, credit.ProfileURL, observationID).Scan(&imageID); err != nil {
			return Result{}, err
		}
		credit.ProfileImageID = imageID
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
	if _, err = tx.Exec(ctx, `DELETE FROM entity_credit_projections WHERE entity_id=$1`, entityID); err != nil {
		return Result{}, err
	}
	creditIdentities := make([]people.CreditIdentity, 0, len(record.Credits))
	for _, credit := range record.Credits {
		creditIdentities = append(creditIdentities, people.CreditIdentity{Provider: credit.Provider, ProviderPersonID: credit.ProviderPersonID})
	}
	if err = people.LockCreditPersonCanonicalization(ctx, tx, creditIdentities); err != nil {
		return Result{}, err
	}
	for i := range record.Credits {
		credit := &record.Credits[i]
		if err = tx.QueryRow(ctx, `INSERT INTO entity_credit_projections(entity_id,provider,provider_person_id,display_name,credit_type,character_name,department,job,credit_order,profile_image_id,projection_version)VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,'')::uuid,$11) RETURNING person_entity_id::text`, entityID, credit.Provider, credit.ProviderPersonID, credit.DisplayName, credit.CreditType, credit.Character, credit.Department, credit.Job, credit.Order, credit.ProfileImageID, version).Scan(&credit.PersonEntityID); err != nil {
			return Result{}, err
		}
	}
	canonicalCredits, err := people.CanonicalizeEntityCredits(ctx, tx, entityID, version)
	if err != nil {
		return Result{}, err
	}
	record.Credits = make([]Credit, 0, len(canonicalCredits))
	for _, credit := range canonicalCredits {
		record.Credits = append(record.Credits, Credit{
			PersonEntityID: credit.PersonEntityID, Provider: credit.Provider,
			ProviderPersonID: credit.ProviderPersonID, DisplayName: credit.DisplayName,
			CreditType: credit.CreditType, Character: credit.Character, Department: credit.Department,
			Job: credit.Job, Order: credit.Order, ProfileImageID: credit.ProfileImageID,
		})
	}
	if _, err = tx.Exec(ctx, `DELETE FROM entity_rating_projections WHERE entity_id=$1`, entityID); err != nil {
		return Result{}, err
	}
	for _, rating := range record.Ratings {
		if _, err = tx.Exec(ctx, `INSERT INTO entity_rating_projections(entity_id,system,value,scale_min,scale_max,votes,projection_version)VALUES($1,$2,$3,$4,$5,$6,$7)`, entityID, rating.System, rating.Value, rating.ScaleMin, rating.ScaleMax, rating.Votes, version); err != nil {
			return Result{}, err
		}
	}
	if len(record.Credits) > 50 {
		record.Credits = record.Credits[:50]
	}
	if err := persistResources(ctx, tx, entityID, def.Kind, &record); err != nil {
		return Result{}, err
	}
	doc := Document{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: def.Kind, Slug: slug, Display: display, ExternalIDs: record.ExternalIDs, Data: Data{Titles: record.Titles, Overview: record.Overview, Overviews: record.Overviews, Classification: Classification{Format: record.Format, Status: record.Status, Language: record.Language, Countries: countries, Genres: genres, SourceMaterial: record.SourceMaterial}, Lifecycle: Lifecycle{StartDate: record.StartDate, EndDate: record.EndDate}, RuntimeMinutes: record.RuntimeMinutes, EpisodeCount: record.EpisodeCount, SeasonCount: max(record.SeasonCount, len(record.Seasons)), Networks: record.Networks, Studios: record.Studios, Organizations: record.Organizations, Keywords: record.Keywords, Seasons: record.Seasons, Episodes: record.Episodes, Images: publicImages, Ratings: record.Ratings, Credits: record.Credits, Links: record.Links, Videos: record.Videos, Certifications: record.Certifications, Recommendations: record.Recommendations}, Freshness: fresh, Provenance: map[string][]SourceRef{"identity": refs, "data": refs}}
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
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, entityID, def.Kind, slug, changeType, []string{"identity", "detail", "search", "provenance", "seasons", "episodes", "images"}, version, record.PrimaryObservationID, nullableJob(jobID))
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

func storedProviderFallbacks(ctx context.Context, tx pgx.Tx, entityID, kind string, currentProviders map[string]bool) ([]NormalizedRecord, error) {
	providers := make([]string, 0, len(currentProviders))
	for provider := range currentProviders {
		providers = append(providers, provider)
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON(provider) document FROM normalized_records WHERE entity_id=$1 AND entity_kind=$2 AND NOT(provider=ANY($3)) ORDER BY provider,observed_at DESC,id DESC`, entityID, kind, providers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NormalizedRecord{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var record NormalizedRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return nil, err
		}
		if record.Provider != "" && !currentProviders[record.Provider] {
			result = append(result, record)
		}
	}
	return result, rows.Err()
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
	if err := hydrateResources(ctx, runtime, id, &doc); err != nil {
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
