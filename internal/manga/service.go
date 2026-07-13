package manga

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/kitsu"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/myanimelist"
	"github.com/jackc/pgx/v5"
)

const MergeVersion = "manga-combiner/v1"

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

type Service struct{ runtime *platform.Runtime }

func NewService(r *platform.Runtime) *Service { return &Service{runtime: r} }

type kitsuEnvelope struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			CanonicalTitle string            `json:"canonicalTitle"`
			Titles         map[string]string `json:"titles"`
			Synopsis       string            `json:"synopsis"`
			Subtype        string            `json:"subtype"`
			Status         string            `json:"status"`
			StartDate      string            `json:"startDate"`
			EndDate        string            `json:"endDate"`
			ChapterCount   int               `json:"chapterCount"`
			VolumeCount    int               `json:"volumeCount"`
			Serialization  string            `json:"serialization"`
			AverageRating  string            `json:"averageRating"`
			UserCount      int               `json:"userCount"`
			PosterImage    imageSet          `json:"posterImage"`
			CoverImage     imageSet          `json:"coverImage"`
		} `json:"attributes"`
	} `json:"data"`
}
type imageSet struct {
	Tiny, Small, Medium, Large, Original string
	Meta                                 struct {
		Dimensions map[string]struct{ Width, Height int }
	} `json:"meta"`
}
type mappingsEnvelope struct {
	Data []struct {
		Attributes struct {
			ExternalSite string `json:"externalSite"`
			ExternalID   string `json:"externalId"`
		} `json:"attributes"`
	} `json:"data"`
}
type malManga struct {
	ID                int                            `json:"id"`
	Title             string                         `json:"title"`
	MainPicture       struct{ Medium, Large string } `json:"main_picture"`
	AlternativeTitles struct {
		Synonyms []string `json:"synonyms"`
		EN, JA   string
	} `json:"alternative_titles"`
	StartDate       string                  `json:"start_date"`
	EndDate         string                  `json:"end_date"`
	Synopsis        string                  `json:"synopsis"`
	Mean            float64                 `json:"mean"`
	NumScoringUsers int                     `json:"num_scoring_users"`
	MediaType       string                  `json:"media_type"`
	Status          string                  `json:"status"`
	NumVolumes      int                     `json:"num_volumes"`
	NumChapters     int                     `json:"num_chapters"`
	Genres          []struct{ Name string } `json:"genres"`
}

func (s *Service) IngestKitsu(ctx context.Context, id string, jobID int64, credentials providercredentials.Credentials) (result Document, returnErr error) {
	id = strings.TrimSpace(id)
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return result, fmt.Errorf("invalid Kitsu manga ID")
	}
	if jobID > 0 {
		_, returnErr = s.runtime.DB.Exec(ctx, `INSERT INTO manga_ingestion_runs(river_job_id,kitsu_manga_id,state)VALUES($1,$2,'working')ON CONFLICT(river_job_id)DO UPDATE SET state='working',error=NULL,completed_at=NULL`, jobID, id)
		if returnErr != nil {
			return
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE manga_ingestion_runs SET state='failed',error=$2,completed_at=now()WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}
	base := kitsu.New(s.runtime.Config.Providers.Kitsu)
	cache, err := providercache.New(s.runtime, "kitsu-manga/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := kitsu.NewCached(s.runtime.Config.Providers.Kitsu, cache).Collect(ctx, providers.Identifier{Provider: "kitsu", Namespace: "manga", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("Kitsu returned no manga payload")
	}
	if payloads[0].StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "kitsu", StatusCode: payloads[0].StatusCode}
	}
	var source kitsuEnvelope
	if err = json.Unmarshal(payloads[0].Body, &source); err != nil {
		return result, err
	}
	if source.Data.ID == "" || strings.TrimSpace(source.Data.Attributes.CanonicalTitle) == "" {
		return result, fmt.Errorf("Kitsu manga response is incomplete")
	}
	var mappings mappingsEnvelope
	if len(payloads) > 1 && payloads[1].StatusCode == http.StatusOK {
		_ = json.Unmarshal(payloads[1].Body, &mappings)
	}
	var mal *malManga
	var malPayload *providers.Payload
	malID := mappingID(mappings, "myanimelist/manga")
	clientID := credentials.APIKey("myanimelist")
	if clientID == "" {
		clientID = s.runtime.Config.Providers.MyAnimeList.ClientID
	}
	if malID != "" && clientID != "" {
		mb := myanimelist.New(s.runtime.Config.Providers.MyAnimeList, clientID)
		mc, e := providercache.New(s.runtime, "myanimelist-manga/v1", mb.Capability().RawRetention, mb.Capability().ResponseCache, jobID)
		if e == nil {
			values, e := myanimelist.NewCached(s.runtime.Config.Providers.MyAnimeList, mc, clientID).Collect(ctx, providers.Identifier{Provider: "myanimelist", Namespace: "manga", Value: malID})
			if e == nil && len(values) > 0 && values[0].StatusCode == http.StatusOK {
				var parsed malManga
				if json.Unmarshal(values[0].Body, &parsed) == nil {
					mal = &parsed
					malPayload = &values[0]
				}
			}
		}
	}
	return s.persist(ctx, id, source, mappings, payloads, mal, malPayload, jobID)
}

func (s *Service) persist(ctx context.Context, id string, source kitsuEnvelope, mappings mappingsEnvelope, payloads []providers.Payload, mal *malManga, malPayload *providers.Payload, jobID int64) (Document, error) {
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	a := source.Data.Attributes
	entityID, slug, created, err := ensureEntity(ctx, tx, id, slugify(a.CanonicalTitle, year(a.StartDate)), payloads[0])
	if err != nil {
		return Document{}, err
	}
	if err = persistNormalized(ctx, tx, entityID, "kitsu", "manga", id, payloads[0], source, "kitsu-manga/v1"); err != nil {
		return Document{}, err
	}
	if len(payloads) > 1 && payloads[1].ObservationID != "" {
		if err = persistNormalized(ctx, tx, entityID, "kitsu", "manga_mappings", id, payloads[1], mappings, "kitsu-mappings/v1"); err != nil {
			return Document{}, err
		}
	}
	external := []ExternalID{{Provider: "kitsu", Namespace: "manga", Value: id}}
	for _, m := range mappings.Data {
		provider, namespace := mappingNamespace(m.Attributes.ExternalSite)
		if provider == "" || m.Attributes.ExternalID == "" {
			continue
		}
		external = appendUniqueID(external, ExternalID{Provider: provider, Namespace: namespace, Value: m.Attributes.ExternalID})
		if _, err = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'manga',$2,$3,$4,'accepted',$5,$6,$6)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, provider, namespace, m.Attributes.ExternalID, payloads[min(1, len(payloads)-1)].ObservationID, payloads[0].ObservedAt); err != nil {
			return Document{}, err
		}
	}
	if malPayload != nil {
		if err = persistNormalized(ctx, tx, entityID, "myanimelist", "manga", strconv.Itoa(mal.ID), *malPayload, *mal, "myanimelist-manga/v1"); err != nil {
			return Document{}, err
		}
	}
	var version int64
	if err = tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now()WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return Document{}, err
	}
	now := time.Now().UTC()
	fresh := Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(7 * 24 * time.Hour)}
	doc := Document{SchemaVersion: 1, ProjectionVersion: version, ID: entityID, Kind: "manga", Slug: slug, ExternalIDs: external, Freshness: fresh, Provenance: map[string][]SourceRef{"spine": {{Provider: "kitsu", ObservationID: payloads[0].ObservationID}}}}
	doc.Display.Title = a.CanonicalTitle
	doc.Display.OriginalTitle = first(a.Titles["ja_jp"], a.Titles["en_jp"])
	doc.Display.Year = year(a.StartDate)
	doc.Data.Titles = kitsuTitles(a)
	doc.Data.Description = a.Synopsis
	doc.Data.Subtype = a.Subtype
	doc.Data.Status = a.Status
	doc.Data.StartDate = a.StartDate
	doc.Data.EndDate = a.EndDate
	doc.Data.VolumeCount = a.VolumeCount
	doc.Data.ChapterCount = a.ChapterCount
	doc.Data.Serialization = a.Serialization
	if rating, _ := strconv.ParseFloat(a.AverageRating, 64); rating > 0 {
		doc.Data.Ratings = append(doc.Data.Ratings, Rating{System: "kitsu", Value: rating, ScaleMin: 0, ScaleMax: 100, Votes: a.UserCount})
	}
	if mal != nil {
		doc.ExternalIDs = appendUniqueID(doc.ExternalIDs, ExternalID{Provider: "myanimelist", Namespace: "manga", Value: strconv.Itoa(mal.ID)})
		doc.Data.Description = first(doc.Data.Description, mal.Synopsis)
		doc.Data.VolumeCount = firstInt(doc.Data.VolumeCount, mal.NumVolumes)
		doc.Data.ChapterCount = firstInt(doc.Data.ChapterCount, mal.NumChapters)
		for _, g := range mal.Genres {
			doc.Data.Genres = append(doc.Data.Genres, g.Name)
		}
		if mal.Mean > 0 {
			doc.Data.Ratings = append(doc.Data.Ratings, Rating{System: "myanimelist", Value: mal.Mean, ScaleMin: 0, ScaleMax: 10, Votes: mal.NumScoringUsers})
		}
		doc.Provenance["supplement"] = []SourceRef{{Provider: "myanimelist", ObservationID: malPayload.ObservationID}}
	}
	doc.Data.Genres = clean(doc.Data.Genres)
	images := []struct {
		class, url string
		set        imageSet
	}{{"poster", a.PosterImage.Original, a.PosterImage}, {"backdrop", a.CoverImage.Original, a.CoverImage}}
	for _, im := range images {
		if im.url == "" {
			continue
		}
		sum := sha256.Sum256([]byte(im.url))
		providerID := hex.EncodeToString(sum[:])
		dims := im.set.Meta.Dimensions["original"]
		var imageID string
		if err = tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,width,height,provider_score,source_observation_id,ownership_scope)VALUES($1,'kitsu',$2,$3,$4,NULLIF($5,0),NULLIF($6,0),90,$7,'entity')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,width=EXCLUDED.width,height=EXCLUDED.height,source_observation_id=EXCLUDED.source_observation_id,ownership_scope='entity' RETURNING id`, entityID, providerID, im.class, im.url, dims.Width, dims.Height, payloads[0].ObservationID).Scan(&imageID); err != nil {
			return Document{}, err
		}
		doc.Data.Images = append(doc.Data.Images, Image{ID: imageID, Class: im.class, Provider: "kitsu", Width: dims.Width, Height: dims.Height})
		if doc.Display.ImageID == "" && im.class == "poster" {
			doc.Display.ImageID = imageID
		}
	}
	body, _ := json.Marshal(doc)
	fingerprint := sha256.Sum256(body)
	if _, err = tx.Exec(ctx, `INSERT INTO canonical_manga(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,source_fingerprint=EXCLUDED.source_fingerprint,document=EXCLUDED.document,updated_at=now()`, entityID, MergeVersion, hex.EncodeToString(fingerprint[:]), body); err != nil {
		return Document{}, err
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "manga", "slug": slug, "display": doc.Display, "freshness": fresh})
	for kind, value := range map[string][]byte{"detail": body, "summary": summary} {
		if _, err = tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,$2,1,$3,$4,$5)ON CONFLICT(entity_id,document_kind)DO UPDATE SET projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, kind, version, value, fresh.FreshUntil); err != nil {
			return Document{}, err
		}
	}
	languages := titleLanguages(doc.Data.Titles)
	if _, err = tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,release_year,status,genres,countries,languages,summary,projection_version)VALUES($1,'manga',$2,$3,NULLIF($4,0),$5,$6,'{}',$7,$8,$9)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,release_year=EXCLUDED.release_year,status=EXCLUDED.status,genres=EXCLUDED.genres,languages=EXCLUDED.languages,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, doc.Display.Title, doc.Display.Year, doc.Data.Status, doc.Data.Genres, languages, summary, version); err != nil {
		return Document{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, entityID); err != nil {
		return Document{}, err
	}
	for _, title := range doc.Data.Titles {
		if _, err = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,locale,source_quality)VALUES($1,$2,lower(unaccent($2)),'title',$3,$4)ON CONFLICT DO NOTHING`, entityID, title.Value, title.Language, map[bool]int{true: 100, false: 80}[title.Primary]); err != nil {
			return Document{}, err
		}
	}
	change := "updated"
	if created {
		change = "created"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id)VALUES($1,'manga',$2,$3,$4,$5,$6,$7)`, entityID, slug, change, []string{"identity", "detail", "search", "provenance"}, version, payloads[0].ObservationID, nullable(jobID)); err != nil {
		return Document{}, err
	}
	if jobID > 0 {
		if _, err = tx.Exec(ctx, `UPDATE manga_ingestion_runs SET state='completed',entity_id=$2,error=NULL,completed_at=now()WHERE river_job_id=$1`, jobID, entityID); err != nil {
			return Document{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (s *Service) Detail(ctx context.Context, id string) (Document, bool, error) {
	var body []byte
	var until time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, id).Scan(&body, &until); err != nil {
		return Document{}, false, err
	}
	var d Document
	if err := json.Unmarshal(body, &d); err != nil {
		return d, false, err
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, id)
	return d, time.Now().Before(until), nil
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	var id string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='manga' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, strings.ToLower(provider), strings.ToLower(namespace), strings.TrimSpace(value)).Scan(&id)
	return id, err
}
func (s *Service) KitsuID(ctx context.Context, id string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='manga' AND provider='kitsu' AND namespace='manga' AND state='accepted'`, id).Scan(&value)
	return value, err
}

func ensureEntity(ctx context.Context, tx pgx.Tx, value, base string, p providers.Payload) (id, slug string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='manga' AND provider='kitsu' AND namespace='manga' AND normalized_value=$1 AND state='accepted'`, value).Scan(&id)
	if err == pgx.ErrNoRows {
		created = true
		for i := 0; ; i++ {
			slug = base
			if i > 0 {
				slug += fmt.Sprintf("-%d", i+2)
			}
			err = tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('manga',$1)ON CONFLICT DO NOTHING RETURNING id`, slug).Scan(&id)
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,'manga',$2)`, id, slug)
				break
			}
			if err != pgx.ErrNoRows {
				return
			}
		}
	} else if err != nil {
		return
	}
	if slug == "" {
		err = tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1`, id).Scan(&slug)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'manga','kitsu','manga',$2,'accepted',$3,$4,$4)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, id, value, p.ObservationID, p.ObservedAt)
	}
	return
}
func persistNormalized(ctx context.Context, tx pgx.Tx, entityID, provider, namespace, recordID string, p providers.Payload, document any, version string) error {
	body, _ := json.Marshal(document)
	_, err := tx.Exec(ctx, `INSERT INTO normalized_records(entity_id,entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES($1,'manga',$2,$3,$4,$5,$6,1,$7,$8)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET entity_id=EXCLUDED.entity_id,document=EXCLUDED.document`, entityID, provider, namespace, recordID, p.ObservationID, version, body, p.ObservedAt)
	return err
}
func mappingID(m mappingsEnvelope, site string) string {
	for _, v := range m.Data {
		if v.Attributes.ExternalSite == site {
			return v.Attributes.ExternalID
		}
	}
	return ""
}
func mappingNamespace(site string) (string, string) {
	switch site {
	case "myanimelist/manga":
		return "myanimelist", "manga"
	case "anilist/manga":
		return "anilist", "manga"
	}
	return "", ""
}
func kitsuTitles(a struct {
	CanonicalTitle string            `json:"canonicalTitle"`
	Titles         map[string]string `json:"titles"`
	Synopsis       string            `json:"synopsis"`
	Subtype        string            `json:"subtype"`
	Status         string            `json:"status"`
	StartDate      string            `json:"startDate"`
	EndDate        string            `json:"endDate"`
	ChapterCount   int               `json:"chapterCount"`
	VolumeCount    int               `json:"volumeCount"`
	Serialization  string            `json:"serialization"`
	AverageRating  string            `json:"averageRating"`
	UserCount      int               `json:"userCount"`
	PosterImage    imageSet          `json:"posterImage"`
	CoverImage     imageSet          `json:"coverImage"`
}) []Text {
	language := map[string]string{"en": "en", "en_us": "en-US", "en_jp": "ja-Latn", "ja_jp": "ja"}
	out := []Text{}
	for key, value := range a.Titles {
		if strings.TrimSpace(value) != "" {
			out = append(out, Text{Value: value, Language: language[key], Type: "title", Primary: value == a.CanonicalTitle})
		}
	}
	if len(out) == 0 {
		out = append(out, Text{Value: a.CanonicalTitle, Type: "title", Primary: true})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Primary && !out[j].Primary || out[i].Language < out[j].Language })
	return out
}
func appendUniqueID(v []ExternalID, id ExternalID) []ExternalID {
	for _, got := range v {
		if got == id {
			return v
		}
	}
	return append(v, id)
}
func clean(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range v {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
func titleLanguages(v []Text) []string {
	out := []string{}
	for _, t := range v {
		if t.Language != "" {
			out = append(out, t.Language)
		}
	}
	return clean(out)
}
func slugify(v string, y int) string {
	v = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(v), "-"), "-")
	if v == "" {
		v = "manga"
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
func first(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
func firstInt(v ...int) int {
	for _, n := range v {
		if n > 0 {
			return n
		}
	}
	return 0
}
func nullable(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
