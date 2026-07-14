package books

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
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/googlebooks"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openlibrary"
	"github.com/jackc/pgx/v5"
)

const MergeVersion = "publication-combiner/v3"

const (
	KindBook        = "book_work"
	KindMangaVolume = "manga_volume"
	KindComicVolume = "comic_volume"
)

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)
var seriesPosition = regexp.MustCompile(`(?i)^(.*?)\s*[\(\[]?\s*(?:#|book\s*|volume\s*|vol\.?\s*|bk\.?\s*|bd\.?\s*)([0-9]+(?:\.[0-9]+)?[a-z]?)\s*[\)\]]?$`)

type Service struct{ runtime *platform.Runtime }

func NewService(r *platform.Runtime) *Service { return &Service{runtime: r} }

func ValidWorkKind(kind string) bool {
	return kind == KindBook || kind == KindMangaVolume || kind == KindComicVolume
}

func EditionKind(kind string) string {
	return map[string]string{KindBook: "book_edition", KindMangaVolume: "manga_edition", KindComicVolume: "comic_edition"}[kind]
}

func Medium(kind string) string {
	return map[string]string{KindBook: "book", KindMangaVolume: "manga", KindComicVolume: "comic"}[kind]
}

type olWork struct {
	Key, Title, Subtitle string
	Description          any      `json:"description"`
	Covers               []int64  `json:"covers"`
	Subjects             []string `json:"subjects"`
	FirstPublishDate     string   `json:"first_publish_date"`
	Authors              []struct {
		Author struct {
			Key string `json:"key"`
		} `json:"author"`
	} `json:"authors"`
}
type olAuthor struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	PersonalName   string   `json:"personal_name"`
	BirthDate      string   `json:"birth_date"`
	DeathDate      string   `json:"death_date"`
	AlternateNames []string `json:"alternate_names"`
}
type olEditions struct {
	Entries []olEdition `json:"entries"`
}
type olEdition struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	PublishDate string   `json:"publish_date"`
	Publishers  []string `json:"publishers"`
	ISBN10      []string `json:"isbn_10"`
	ISBN13      []string `json:"isbn_13"`
	Languages   []struct {
		Key string `json:"key"`
	} `json:"languages"`
	Works []struct {
		Key string `json:"key"`
	} `json:"works"`
	Covers         []int64  `json:"covers"`
	NumberOfPages  int      `json:"number_of_pages"`
	PhysicalFormat string   `json:"physical_format"`
	Series         []string `json:"series"`
}
type googleVolume struct {
	ID         string `json:"id"`
	VolumeInfo struct {
		Title, Subtitle, Description, PublishedDate, Publisher string
		Authors, Categories                                    []string
		PageCount                                              int     `json:"pageCount"`
		AverageRating                                          float64 `json:"averageRating"`
		RatingsCount                                           int     `json:"ratingsCount"`
		IndustryIdentifiers                                    []struct{ Type, Identifier string }
		ImageLinks                                             map[string]string `json:"imageLinks"`
	} `json:"volumeInfo"`
}
type googleVolumes struct {
	Items []googleVolume `json:"items"`
}

func (s *Service) IngestWork(ctx context.Context, key string, jobID int64, credentials providercredentials.Credentials) (result Document, returnErr error) {
	return s.IngestWorkAs(ctx, key, KindBook, jobID, credentials)
}

func (s *Service) IngestWorkAs(ctx context.Context, key, kind string, jobID int64, credentials providercredentials.Credentials) (result Document, returnErr error) {
	return s.IngestWorkEditionAs(ctx, key, "", kind, jobID, credentials)
}

// IngestWorkEditionAs refreshes the bounded work catalog and optionally forces
// one selected Open Library edition through the same canonical pipeline. This
// avoids crawling every edition of a popular work merely to resolve one client
// selection.
func (s *Service) IngestWorkEditionAs(ctx context.Context, key, editionKey, kind string, jobID int64, credentials providercredentials.Credentials) (result Document, returnErr error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	editionKey = strings.ToUpper(strings.TrimSpace(editionKey))
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !strings.HasPrefix(key, "OL") || !strings.HasSuffix(key, "W") {
		return result, fmt.Errorf("invalid Open Library work key")
	}
	if !ValidWorkKind(kind) {
		return result, fmt.Errorf("invalid publication kind %q", kind)
	}
	if editionKey != "" && (!strings.HasPrefix(editionKey, "OL") || !strings.HasSuffix(editionKey, "M")) {
		return result, fmt.Errorf("invalid Open Library edition key")
	}
	if jobID > 0 {
		_, returnErr = s.runtime.DB.Exec(ctx, `INSERT INTO book_ingestion_runs(river_job_id,openlibrary_work_id,entity_kind,state)VALUES($1,$2,$3,'working')ON CONFLICT(river_job_id)DO UPDATE SET entity_kind=EXCLUDED.entity_kind,state='working',error=NULL,completed_at=NULL`, jobID, key, kind)
		if returnErr != nil {
			return result, returnErr
		}
		defer func() {
			if returnErr != nil {
				_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE book_ingestion_runs SET state='failed',error=$2,completed_at=now() WHERE river_job_id=$1`, jobID, returnErr.Error())
			}
		}()
	}
	base := openlibrary.New(s.runtime.Config.Providers.OpenLibrary)
	cache, err := providercache.New(s.runtime, "openlibrary-book/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	client := openlibrary.NewCached(s.runtime.Config.Providers.OpenLibrary, cache)
	workPayloads, err := client.Collect(ctx, providers.Identifier{Provider: "openlibrary", Namespace: "work", Value: key})
	if err != nil {
		return result, err
	}
	if len(workPayloads) == 0 {
		return result, fmt.Errorf("Open Library returned no work payload")
	}
	if workPayloads[0].StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "openlibrary", StatusCode: workPayloads[0].StatusCode}
	}
	var work olWork
	if err = json.Unmarshal(workPayloads[0].Body, &work); err != nil {
		return result, err
	}
	work.Key = trimKey(work.Key)
	if work.Key == "" {
		work.Key = key
	}
	edPayload, err := client.Editions(ctx, key, 50)
	if err != nil {
		return result, err
	}
	var editions olEditions
	if edPayload.StatusCode == http.StatusOK {
		_ = json.Unmarshal(edPayload.Body, &editions)
	}
	editionPayloads := map[string]providers.Payload{}
	if editionKey != "" && !containsEdition(editions.Entries, editionKey) {
		payloads, collectErr := client.Collect(ctx, providers.Identifier{Provider: "openlibrary", Namespace: "edition", Value: editionKey})
		if collectErr != nil {
			return result, collectErr
		}
		if len(payloads) == 0 || payloads[0].StatusCode != http.StatusOK {
			return result, fmt.Errorf("Open Library edition %s was not found", editionKey)
		}
		var edition olEdition
		if unmarshalErr := json.Unmarshal(payloads[0].Body, &edition); unmarshalErr != nil {
			return result, unmarshalErr
		}
		edition.Key = trimKey(edition.Key)
		if edition.Key == "" {
			edition.Key = editionKey
		}
		if !editionBelongsToWork(edition, key) {
			return result, fmt.Errorf("Open Library edition %s does not belong to work %s", editionKey, key)
		}
		editions.Entries = append(editions.Entries, edition)
		editionPayloads[editionKey] = payloads[0]
	}
	authors := []Author{}
	authorPayloads := map[string]providers.Payload{}
	for _, ref := range work.Authors[:min(len(work.Authors), 20)] {
		ak := trimKey(ref.Author.Key)
		if ak == "" {
			continue
		}
		payloads, e := client.Collect(ctx, providers.Identifier{Provider: "openlibrary", Namespace: "author", Value: ak})
		if e != nil || len(payloads) == 0 || payloads[0].StatusCode != http.StatusOK {
			authors = append(authors, Author{Name: ak, ExternalIDs: []ExternalID{{Provider: "openlibrary", Namespace: "author", Value: ak}}})
			continue
		}
		var a olAuthor
		if json.Unmarshal(payloads[0].Body, &a) == nil {
			authors = append(authors, Author{Name: first(a.Name, a.PersonalName, ak), ExternalIDs: []ExternalID{{Provider: "openlibrary", Namespace: "author", Value: ak}}})
			authorPayloads[ak] = payloads[0]
		}
	}
	google := map[string]googleVolumes{}
	googleObs := map[string]string{}
	gbBase := googlebooks.New(s.runtime.Config.Providers.GoogleBooks)
	gbCache, e := providercache.New(s.runtime, "googlebooks-edition/v1", gbBase.Capability().RawRetention, gbBase.Capability().ResponseCache, jobID)
	if e == nil {
		gb := googlebooks.NewCached(s.runtime.Config.Providers.GoogleBooks, gbCache, credentials.APIKey("googlebooks"))
		for _, ed := range googleEditionCandidates(editions.Entries, editionKey, 15) {
			isbn := firstSlice(ed.ISBN13, ed.ISBN10)
			if isbn == "" {
				continue
			}
			payloads, e := gb.Collect(ctx, providers.Identifier{Provider: "isbn", Namespace: isbnNamespace(isbn), Value: isbn})
			if e == nil && len(payloads) > 0 && payloads[0].StatusCode == http.StatusOK {
				var values googleVolumes
				if json.Unmarshal(payloads[0].Body, &values) == nil {
					if item, ok := exactGoogleVolume(values, isbn); ok {
						values.Items = []googleVolume{item}
						google[trimKey(ed.Key)] = values
						googleObs[trimKey(ed.Key)] = payloads[0].ObservationID
					}
				}
			}
		}
	}
	return s.persist(ctx, key, kind, work, workPayloads[0], editions.Entries, edPayload, editionPayloads, authors, authorPayloads, google, googleObs, jobID)
}

func (s *Service) persist(ctx context.Context, key, kind string, work olWork, wp providers.Payload, editions []olEdition, ep providers.Payload, editionPayloads map[string]providers.Payload, authors []Author, authorPayloads map[string]providers.Payload, google map[string]googleVolumes, googleObs map[string]string, jobID int64) (Document, error) {
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	fresh := Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(14 * 24 * time.Hour)}
	workID, slug, created, err := ensureEntity(ctx, tx, kind, "openlibrary", "work", key, slugify(work.Title, year(work.FirstPublishDate), Medium(kind)), wp.ObservationID, wp.ObservedAt)
	if err != nil {
		return Document{}, err
	}
	if err = persistNormalized(ctx, tx, workID, kind, "openlibrary", "work", key, wp.ObservationID, "openlibrary-publication-work/v2", work, wp.ObservedAt); err != nil {
		return Document{}, err
	}
	if ep.ObservationID != "" {
		if err = persistNormalized(ctx, tx, workID, kind, "openlibrary", "work_editions", key, ep.ObservationID, "openlibrary-publication-editions/v2", editions, ep.ObservedAt); err != nil {
			return Document{}, err
		}
	}
	var version int64
	if version, err = nextProjectionVersion(ctx, tx, workID); err != nil {
		return Document{}, err
	}
	doc := Document{SchemaVersion: 2, ProjectionVersion: version, ID: workID, Kind: kind, Slug: slug, ExternalIDs: []ExternalID{{Provider: "openlibrary", Namespace: "work", Value: key}}, Freshness: fresh, Provenance: map[string][]SourceRef{"work": {{Provider: "openlibrary", ObservationID: wp.ObservationID}}}}
	doc.Data.Publication = PublicationClassification{Medium: Medium(kind), Scope: "work"}
	doc.Display.Title = work.Title
	doc.Display.Year = year(work.FirstPublishDate)
	doc.Data.Subtitle = work.Subtitle
	doc.Data.Description = description(work.Description)
	doc.Data.Subjects = clean(work.Subjects)
	doc.Data.Series = publicationSeries(editions, "work", ep.ObservationID)
	doc.Data.FirstPublishYear = doc.Display.Year
	images, err := s.persistCovers(ctx, tx, workID, work.Covers, "", wp.ObservationID)
	if err != nil {
		return Document{}, err
	}
	doc.Data.Images = images
	if len(images) > 0 {
		doc.Display.ImageID = images[0].ID
	}
	for i, a := range authors {
		ak := ""
		if len(a.ExternalIDs) > 0 {
			ak = a.ExternalIDs[0].Value
		}
		payload := authorPayloads[ak]
		aid, aslug, _, e := ensureEntity(ctx, tx, "author", "openlibrary", "author", ak, slugify(a.Name, 0, "author"), payload.ObservationID, chooseTime(payload.ObservedAt, wp.ObservedAt))
		if e != nil {
			return Document{}, e
		}
		a.ID = aid
		authors[i] = a
		if payload.ObservationID != "" {
			if e = persistNormalized(ctx, tx, aid, "author", "openlibrary", "author", ak, payload.ObservationID, "openlibrary-author/v1", a, payload.ObservedAt); e != nil {
				return Document{}, e
			}
		}
		authorVersion, versionErr := nextProjectionVersion(ctx, tx, aid)
		if versionErr != nil {
			return Document{}, versionErr
		}
		if e = upsertAuthorProjection(ctx, tx, aid, aslug, a, fresh, authorVersion); e != nil {
			return Document{}, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO book_work_authors(work_entity_id,author_entity_id,position)VALUES($1,$2,$3)ON CONFLICT(work_entity_id,author_entity_id) DO UPDATE SET position=EXCLUDED.position`, workID, aid, i); e != nil {
			return Document{}, fmt.Errorf("relate work author: %w", e)
		}
	}
	doc.Data.Authors = authors
	for _, ed := range editions {
		ek := trimKey(ed.Key)
		if ek == "" {
			continue
		}
		editionPayload := ep
		if targeted, ok := editionPayloads[ek]; ok {
			editionPayload = targeted
		}
		editionObservedAt := chooseTime(editionPayload.ObservedAt, ep.ObservedAt)
		editionKind := EditionKind(kind)
		eid, eslug, _, e := ensureEntity(ctx, tx, editionKind, "openlibrary", "edition", ek, slugify(first(ed.Title, work.Title), year(ed.PublishDate), Medium(kind)+"-edition"), editionPayload.ObservationID, editionObservedAt)
		if e != nil {
			return Document{}, e
		}
		if _, targeted := editionPayloads[ek]; targeted {
			if e = persistNormalized(ctx, tx, eid, editionKind, "openlibrary", "edition", ek, editionPayload.ObservationID, "openlibrary-publication-edition/v2", ed, editionObservedAt); e != nil {
				return Document{}, e
			}
		}
		editionVersion, versionErr := nextProjectionVersion(ctx, tx, eid)
		if versionErr != nil {
			return Document{}, versionErr
		}
		summary := EditionSummary{ID: eid, Title: first(ed.Title, work.Title), PublishedDate: ed.PublishDate, Publishers: ed.Publishers, Languages: languageKeys(ed.Languages), ISBN10: ed.ISBN10, ISBN13: ed.ISBN13, Format: ed.PhysicalFormat, PageCount: ed.NumberOfPages, Series: seriesMemberships(ed.Series, "edition", editionPayload.ObservationID)}
		doc.Data.Editions = append(doc.Data.Editions, summary)
		edoc := Document{SchemaVersion: 2, ProjectionVersion: editionVersion, ID: eid, Kind: editionKind, Slug: eslug, ExternalIDs: []ExternalID{{Provider: "openlibrary", Namespace: "edition", Value: ek}}, Freshness: fresh, Provenance: map[string][]SourceRef{"edition": {{Provider: "openlibrary", ObservationID: editionPayload.ObservationID}}}}
		edoc.Data.Publication = PublicationClassification{Medium: Medium(kind), Scope: "work"}
		edoc.Display.Title = summary.Title
		edoc.Display.Year = year(ed.PublishDate)
		edoc.Data.WorkID = workID
		edoc.Data.Subtitle = ed.Subtitle
		edoc.Data.Authors = authors
		edoc.Data.PublishedDate = ed.PublishDate
		edoc.Data.Publishers = clean(ed.Publishers)
		edoc.Data.Languages = summary.Languages
		edoc.Data.ISBN10 = clean(ed.ISBN10)
		edoc.Data.ISBN13 = clean(ed.ISBN13)
		edoc.Data.Format = ed.PhysicalFormat
		edoc.Data.PageCount = ed.NumberOfPages
		edoc.Data.Series = summary.Series
		editionImages, imageErr := s.persistCovers(ctx, tx, eid, ed.Covers, firstSlice(languageKeys(ed.Languages)), editionPayload.ObservationID)
		if imageErr != nil {
			return Document{}, imageErr
		}
		edoc.Data.Images = editionImages
		if len(editionImages) > 0 {
			edoc.Display.ImageID = editionImages[0].ID
		}
		for _, isbn := range append(append([]string{}, ed.ISBN10...), ed.ISBN13...) {
			ns := isbnNamespace(isbn)
			edoc.ExternalIDs = append(edoc.ExternalIDs, ExternalID{Provider: "isbn", Namespace: ns, Value: normalizeISBN(isbn)})
			if _, e = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,'isbn',$3,$4,'accepted',$5,$6,$6)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, eid, editionKind, ns, normalizeISBN(isbn), editionPayload.ObservationID, editionObservedAt); e != nil {
				return Document{}, fmt.Errorf("persist edition ISBN: %w", e)
			}
		}
		if values := google[ek]; len(values.Items) > 0 {
			g := values.Items[0]
			if e = persistNormalized(ctx, tx, eid, editionKind, "googlebooks", "volume", g.ID, googleObs[ek], "googlebooks-volume/v1", g, now); e != nil {
				return Document{}, e
			}
			edoc.ExternalIDs = append(edoc.ExternalIDs, ExternalID{Provider: "googlebooks", Namespace: "volume", Value: g.ID})
			edoc.Data.Description = first(edoc.Data.Description, g.VolumeInfo.Description)
			if edoc.Data.PageCount == 0 {
				edoc.Data.PageCount = g.VolumeInfo.PageCount
			}
			edoc.Data.Subjects = clean(append(edoc.Data.Subjects, g.VolumeInfo.Categories...))
			if g.VolumeInfo.AverageRating > 0 {
				edoc.Data.Ratings = append(edoc.Data.Ratings, Rating{System: "googlebooks", Value: g.VolumeInfo.AverageRating, ScaleMin: 0, ScaleMax: 5, Votes: g.VolumeInfo.RatingsCount})
			}
			edoc.Provenance["supplement"] = []SourceRef{{Provider: "googlebooks", ObservationID: googleObs[ek]}}
			if _, e = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,'googlebooks','volume',$3,'accepted',$4,$5,$5)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, eid, editionKind, g.ID, googleObs[ek], now); e != nil {
				return Document{}, fmt.Errorf("persist Google Books volume claim: %w", e)
			}
		}
		eb, _ := json.Marshal(edoc)
		sum := sha256.Sum256(eb)
		_, e = tx.Exec(ctx, `INSERT INTO canonical_book_editions(entity_id,work_entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4,$5)ON CONFLICT(entity_id)DO UPDATE SET document=EXCLUDED.document,source_fingerprint=EXCLUDED.source_fingerprint,updated_at=now()`, eid, workID, MergeVersion, hex.EncodeToString(sum[:]), eb)
		if e != nil {
			return Document{}, e
		}
		if e = upsertProjection(ctx, tx, edoc, authors, edoc.Data.Subjects); e != nil {
			return Document{}, e
		}
	}
	doc.Data.Editions = sortEditions(doc.Data.Editions)
	body, _ := json.Marshal(doc)
	sum := sha256.Sum256(body)
	_, err = tx.Exec(ctx, `INSERT INTO canonical_book_works(entity_id,merge_version,source_fingerprint,document)VALUES($1,$2,$3,$4)ON CONFLICT(entity_id)DO UPDATE SET document=EXCLUDED.document,source_fingerprint=EXCLUDED.source_fingerprint,updated_at=now()`, workID, MergeVersion, hex.EncodeToString(sum[:]), body)
	if err != nil {
		return Document{}, err
	}
	if err = upsertProjection(ctx, tx, doc, authors, doc.Data.Subjects); err != nil {
		return Document{}, err
	}
	change := "updated"
	if created {
		change = "created"
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id,river_job_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, workID, kind, slug, change, []string{"identity", "detail", "search", "provenance"}, version, wp.ObservationID, nullable(jobID))
	if jobID > 0 {
		_, err = tx.Exec(ctx, `UPDATE book_ingestion_runs SET state='completed',entity_id=$2,error=NULL,completed_at=now()WHERE river_job_id=$1`, jobID, workID)
		if err != nil {
			return Document{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (s *Service) Detail(ctx context.Context, id string) (Document, bool, error) {
	var b []byte
	var until time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, id).Scan(&b, &until); err != nil {
		return Document{}, false, err
	}
	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		return d, false, err
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, id)
	return d, time.Now().Before(until), nil
}
func (s *Service) Resolve(ctx context.Context, kind, provider, namespace, value string) (string, error) {
	var id string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, kind, strings.ToLower(provider), strings.ToLower(namespace), normalizeClaim(provider, value)).Scan(&id)
	return id, err
}
func (s *Service) OpenLibraryWorkID(ctx context.Context, id string) (string, error) {
	var v string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind IN('book_work','manga_volume','comic_volume') AND provider='openlibrary' AND namespace='work' AND state='accepted'`, id).Scan(&v)
	return v, err
}

func (s *Service) persistCovers(ctx context.Context, tx pgx.Tx, entityID string, covers []int64, language, observationID string) ([]Image, error) {
	images := []Image{}
	for _, cover := range covers[:min(len(covers), 20)] {
		if cover <= 0 {
			continue
		}
		providerID := strconv.FormatInt(cover, 10)
		sourceURL := strings.TrimRight(s.runtime.Config.Providers.OpenLibrary.CoversBaseURL, "/") + "/b/id/" + providerID + "-L.jpg"
		var imageID string
		if err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,language,source_observation_id)VALUES($1,'openlibrary',$2,'cover',$3,NULLIF($4,''),$5)ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,language=EXCLUDED.language,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, providerID, sourceURL, language, observationID).Scan(&imageID); err != nil {
			return nil, fmt.Errorf("persist Open Library cover: %w", err)
		}
		images = append(images, Image{ID: imageID, Class: "cover", Provider: "openlibrary"})
	}
	return images, nil
}

func ensureEntity(ctx context.Context, tx pgx.Tx, kind, provider, namespace, value, base, obs string, observed time.Time) (id, slug string, created bool, err error) {
	value = normalizeClaim(provider, value)
	err = tx.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4 AND state='accepted'`, kind, provider, namespace, value).Scan(&id)
	if err == pgx.ErrNoRows {
		created = true
		for i := 0; ; i++ {
			slug = base
			if i > 0 {
				slug += fmt.Sprintf("-%d", i+2)
			}
			err = tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES($1,$2)ON CONFLICT DO NOTHING RETURNING id`, kind, slug).Scan(&id)
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,$2,$3)`, id, kind, slug)
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
	if err != nil {
		return
	}
	if obs != "" {
		_, err = tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,$3,$4,$5,'accepted',$6,$7,$7)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, id, kind, provider, namespace, value, obs, observed)
	}
	return
}
func upsertProjection(ctx context.Context, tx pgx.Tx, d Document, authors []Author, subjects []string) error {
	if subjects == nil {
		subjects = []string{}
	}
	b, _ := json.Marshal(d)
	summary := Summary{SchemaVersion: d.SchemaVersion, ProjectionVersion: d.ProjectionVersion, ID: d.ID, Kind: d.Kind, Slug: d.Slug, Authors: authors, Subjects: subjects, Freshness: d.Freshness}
	summary.Display = d.Display
	sb, _ := json.Marshal(summary)
	for k, v := range map[string][]byte{"detail": b, "summary": sb} {
		if _, err := tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,$2,$3,$4,$5,$6)ON CONFLICT(entity_id,document_kind)DO UPDATE SET schema_version=EXCLUDED.schema_version,projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, d.ID, k, d.SchemaVersion, d.ProjectionVersion, v, d.Freshness.FreshUntil); err != nil {
			return err
		}
	}
	countries := []string{}
	languages := d.Data.Languages
	if languages == nil {
		languages = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,release_year,genres,countries,languages,summary,projection_version)VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9,$10)ON CONFLICT(entity_id)DO UPDATE SET display_title=EXCLUDED.display_title,release_year=EXCLUDED.release_year,genres=EXCLUDED.genres,languages=EXCLUDED.languages,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, d.ID, d.Kind, d.Slug, d.Display.Title, d.Display.Year, subjects, countries, languages, sb, d.ProjectionVersion)
	if err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, d.ID)
	names := []string{d.Display.Title}
	for _, a := range authors {
		names = append(names, a.Name)
	}
	for i, n := range clean(names) {
		_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),$3,$4)ON CONFLICT DO NOTHING`, d.ID, n, map[bool]string{true: "title", false: "author"}[i == 0], map[bool]int{true: 100, false: 70}[i == 0])
	}
	return nil
}

func nextProjectionVersion(ctx context.Context, tx pgx.Tx, entityID string) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return 0, fmt.Errorf("increment %s projection version: %w", entityID, err)
	}
	return version, nil
}

func upsertAuthorProjection(ctx context.Context, tx pgx.Tx, entityID, slug string, author Author, fresh Freshness, version int64) error {
	detail, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "author", "slug": slug, "display": map[string]any{"name": author.Name}, "external_ids": author.ExternalIDs, "freshness": fresh})
	if _, err := tx.Exec(ctx, `INSERT INTO canonical_authors(entity_id,merge_version,document)VALUES($1,$2,$3)ON CONFLICT(entity_id)DO UPDATE SET merge_version=EXCLUDED.merge_version,document=EXCLUDED.document,updated_at=now()`, entityID, MergeVersion, detail); err != nil {
		return fmt.Errorf("persist canonical author: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,'detail',1,$2,$3,$4)ON CONFLICT(entity_id,document_kind)DO UPDATE SET schema_version=EXCLUDED.schema_version,projection_version=EXCLUDED.projection_version,document=EXCLUDED.document,fresh_until=EXCLUDED.fresh_until,updated_at=now()`, entityID, version, detail, fresh.FreshUntil); err != nil {
		return fmt.Errorf("persist author projection: %w", err)
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "author", "slug": slug, "display": map[string]any{"name": author.Name}, "freshness": fresh})
	empty := []string{}
	if _, err := tx.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,genres,countries,languages,summary,projection_version)VALUES($1,'author',$2,$3,$4,$4,$4,$5,$6)ON CONFLICT(entity_id)DO UPDATE SET slug=EXCLUDED.slug,display_title=EXCLUDED.display_title,summary=EXCLUDED.summary,projection_version=EXCLUDED.projection_version,updated_at=now()`, entityID, slug, author.Name, empty, summary, version); err != nil {
		return fmt.Errorf("persist author search entity: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),'name',100)ON CONFLICT DO NOTHING`, entityID, author.Name); err != nil {
		return fmt.Errorf("persist author search name: %w", err)
	}
	return nil
}
func slugify(v string, y int, fallback string) string {
	v = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(v), "-"), "-")
	if v == "" {
		v = fallback
	}
	if y > 0 {
		v += "-" + strconv.Itoa(y)
	}
	return v
}
func trimKey(v string) string {
	p := strings.Split(strings.TrimSpace(v), "/")
	return strings.ToUpper(p[len(p)-1])
}
func year(v string) int {
	if len(v) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(v[:4])
	return n
}
func description(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if s, ok := x["value"].(string); ok {
			return s
		}
	}
	return ""
}
func first(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
func clean(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range v {
		s = strings.TrimSpace(s)
		k := strings.ToLower(s)
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
func firstSlice(v ...[]string) string {
	for _, a := range v {
		if len(a) > 0 {
			return a[0]
		}
	}
	return ""
}
func isbnNamespace(v string) string {
	if len(normalizeISBN(v)) == 10 {
		return "isbn10"
	}
	return "isbn13"
}
func normalizeISBN(v string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(v))
}
func normalizeClaim(provider, v string) string {
	v = strings.TrimSpace(v)
	if provider == "openlibrary" {
		return strings.ToUpper(trimKey(v))
	}
	if provider == "isbn" {
		return normalizeISBN(v)
	}
	return v
}
func languageKeys(v []struct {
	Key string `json:"key"`
}) []string {
	out := []string{}
	for _, x := range v {
		out = append(out, strings.ToLower(trimKey(x.Key)))
	}
	return clean(out)
}
func chooseTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	return a
}
func nullable(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
func sortEditions(v []EditionSummary) []EditionSummary {
	sort.SliceStable(v, func(i, j int) bool { return v[i].PublishedDate < v[j].PublishedDate })
	return v
}

func publicationSeries(editions []olEdition, scope, observationID string) []SeriesMembership {
	values := make([]string, 0)
	for _, edition := range editions {
		values = append(values, edition.Series...)
	}
	return seriesMemberships(values, scope, observationID)
}

func seriesMemberships(values []string, scope, observationID string) []SeriesMembership {
	result := make([]SeriesMembership, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		name := strings.TrimSpace(value)
		position := ""
		if matches := seriesPosition.FindStringSubmatch(name); len(matches) == 3 && strings.TrimSpace(matches[1]) != "" {
			name = strings.TrimSpace(strings.TrimRight(matches[1], ",;:-"))
			position = strings.TrimSpace(matches[2])
		}
		key := strings.ToLower(name) + "\x00" + strings.ToLower(position)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, SeriesMembership{Name: name, Position: position, Provider: "openlibrary", Scope: scope, ObservationID: observationID, ResolutionState: "unresolved"})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if strings.EqualFold(result[i].Name, result[j].Name) {
			return result[i].Position < result[j].Position
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}
func containsEdition(editions []olEdition, editionKey string) bool {
	for _, edition := range editions {
		if trimKey(edition.Key) == editionKey {
			return true
		}
	}
	return false
}
func editionBelongsToWork(edition olEdition, workKey string) bool {
	for _, work := range edition.Works {
		if trimKey(work.Key) == workKey {
			return true
		}
	}
	return false
}
func googleEditionCandidates(editions []olEdition, preferred string, limit int) []olEdition {
	if limit < 1 {
		return nil
	}
	result := make([]olEdition, 0, min(len(editions), limit))
	if preferred != "" {
		for _, edition := range editions {
			if trimKey(edition.Key) == preferred {
				result = append(result, edition)
				break
			}
		}
	}
	for _, edition := range editions {
		if len(result) >= limit {
			break
		}
		if preferred != "" && trimKey(edition.Key) == preferred {
			continue
		}
		result = append(result, edition)
	}
	return result
}
func persistNormalized(ctx context.Context, tx pgx.Tx, entityID, kind, provider, namespace, providerID, observationID, version string, document any, observed time.Time) error {
	body, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO normalized_records(entity_id,entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET entity_id=EXCLUDED.entity_id,entity_kind=EXCLUDED.entity_kind,provider=EXCLUDED.provider,provider_namespace=EXCLUDED.provider_namespace,provider_record_id=EXCLUDED.provider_record_id,document=EXCLUDED.document,observed_at=EXCLUDED.observed_at`, entityID, kind, provider, namespace, providerID, observationID, version, body, observed)
	return err
}
func exactGoogleVolume(values googleVolumes, isbn string) (googleVolume, bool) {
	want := normalizeISBN(isbn)
	var best googleVolume
	bestScore := -1
	for _, item := range values.Items {
		for _, id := range item.VolumeInfo.IndustryIdentifiers {
			if normalizeISBN(id.Identifier) == want {
				score := googleVolumeEvidenceScore(item)
				if score > bestScore || (score == bestScore && item.ID < best.ID) {
					best, bestScore = item, score
				}
				break
			}
		}
	}
	return best, bestScore >= 0
}

func googleVolumeEvidenceScore(item googleVolume) int {
	score := 0
	if item.VolumeInfo.AverageRating > 0 {
		score += 10_000 + min(item.VolumeInfo.RatingsCount, 9_999)
	}
	if item.VolumeInfo.Description != "" {
		score += 100
	}
	if item.VolumeInfo.PageCount > 0 {
		score += 20
	}
	if len(item.VolumeInfo.ImageLinks) > 0 {
		score += 10
	}
	score += min(len(item.VolumeInfo.Categories), 9)
	return score
}
