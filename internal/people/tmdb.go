package people

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
)

const tmdbPersonNormalizerVersion = "tmdb-person/v1"

type tmdbPerson struct {
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	AlsoKnownAs        []string `json:"also_known_as"`
	Biography          string   `json:"biography"`
	Birthday           string   `json:"birthday"`
	Deathday           string   `json:"deathday"`
	Gender             int      `json:"gender"`
	PlaceOfBirth       string   `json:"place_of_birth"`
	KnownForDepartment string   `json:"known_for_department"`
	Homepage           string   `json:"homepage"`
	Popularity         float64  `json:"popularity"`
	ProfilePath        string   `json:"profile_path"`
	ExternalIDs        struct {
		IMDbID      string `json:"imdb_id"`
		WikidataID  string `json:"wikidata_id"`
		FacebookID  string `json:"facebook_id"`
		InstagramID string `json:"instagram_id"`
		TwitterID   string `json:"twitter_id"`
		YouTubeID   string `json:"youtube_id"`
		TikTokID    string `json:"tiktok_id"`
	} `json:"external_ids"`
	Images struct {
		Profiles []struct {
			FilePath    string  `json:"file_path"`
			Width       int     `json:"width"`
			Height      int     `json:"height"`
			VoteAverage float64 `json:"vote_average"`
		} `json:"profiles"`
	} `json:"images"`
	CombinedCredits struct {
		Cast []tmdbPersonCredit `json:"cast"`
		Crew []tmdbPersonCredit `json:"crew"`
	} `json:"combined_credits"`
}

type tmdbPersonCredit struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	Character    string `json:"character"`
	Department   string `json:"department"`
	Job          string `json:"job"`
	Order        int    `json:"order"`
	EpisodeCount int    `json:"episode_count"`
	PosterPath   string `json:"poster_path"`
}

func (s *Service) EnrichTMDB(ctx context.Context, entityID, tmdbID string, jobID int64, credentials providercredentials.Credentials) error {
	if _, err := strconv.ParseInt(tmdbID, 10, 64); err != nil {
		return fmt.Errorf("invalid TMDB person ID %q", tmdbID)
	}
	var claimed bool
	if err := s.runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND provider='tmdb' AND namespace='person' AND normalized_value=$2 AND state='accepted')`, entityID, tmdbID).Scan(&claimed); err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("TMDB person %s is not claimed by canonical person %s", tmdbID, entityID)
	}
	capability := tmdb.PersonCapability()
	cache, err := providercache.New(s.runtime, tmdbPersonNormalizerVersion, capability.RawRetention, capability.ResponseCache, jobID)
	if err != nil {
		return err
	}
	payloads, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, cache, credentials.APIKey("tmdb")).CollectPerson(ctx, providers.Identifier{Provider: "tmdb", Namespace: "person", Value: tmdbID})
	if err != nil {
		return err
	}
	if len(payloads) == 0 {
		return fmt.Errorf("TMDB returned no person detail")
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	var person tmdbPerson
	if err := json.Unmarshal(payload.Body, &person); err != nil {
		return fmt.Errorf("decode TMDB person: %w", err)
	}
	if person.ID < 1 || strings.TrimSpace(person.Name) == "" {
		return fmt.Errorf("invalid TMDB person detail")
	}
	return s.persistTMDB(ctx, entityID, payload, person, jobID)
}

func (s *Service) persistTMDB(ctx context.Context, entityID string, payload providers.Payload, person tmdbPerson, jobID int64) error {
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	normalizedBody, _ := json.Marshal(person)
	var normalizedID string
	if err := tx.QueryRow(ctx, `INSERT INTO normalized_records(entity_id,entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES($1,'person','tmdb','person',$2,$3,$4,1,$5,$6)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET document=EXCLUDED.document,entity_id=EXCLUDED.entity_id RETURNING id`, entityID, strconv.FormatInt(person.ID, 10), payload.ObservationID, tmdbPersonNormalizerVersion, normalizedBody, payload.ObservedAt).Scan(&normalizedID); err != nil {
		return err
	}

	primaryImageID := ""
	profiles := person.Images.Profiles
	if person.ProfilePath != "" {
		profiles = append([]struct {
			FilePath    string  `json:"file_path"`
			Width       int     `json:"width"`
			Height      int     `json:"height"`
			VoteAverage float64 `json:"vote_average"`
		}{{FilePath: person.ProfilePath}}, profiles...)
	}
	seenProfiles := map[string]bool{}
	for _, profile := range profiles {
		if profile.FilePath == "" || seenProfiles[profile.FilePath] {
			continue
		}
		seenProfiles[profile.FilePath] = true
		var imageID string
		if err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,width,height,provider_score,source_observation_id,ownership_scope)VALUES($1,'tmdb',$2,'profile',$3,NULLIF($4,0),NULLIF($5,0),NULLIF($6,0),$7,'person_profile')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,width=EXCLUDED.width,height=EXCLUDED.height,provider_score=EXCLUDED.provider_score,source_observation_id=EXCLUDED.source_observation_id,ownership_scope='person_profile' RETURNING id`, entityID, profile.FilePath, "https://image.tmdb.org/t/p/original"+profile.FilePath, profile.Width, profile.Height, profile.VoteAverage, payload.ObservationID).Scan(&imageID); err != nil {
			return err
		}
		if primaryImageID == "" {
			primaryImageID = imageID
		}
	}

	claims := []PersonExternalID{{Provider: "tmdb", Namespace: "person", Value: strconv.FormatInt(person.ID, 10)}}
	for _, claim := range []PersonExternalID{
		{Provider: "imdb", Namespace: "name", Value: person.ExternalIDs.IMDbID},
		{Provider: "wikidata", Namespace: "item", Value: strings.ToUpper(person.ExternalIDs.WikidataID)},
		{Provider: "facebook", Namespace: "person", Value: person.ExternalIDs.FacebookID},
		{Provider: "instagram", Namespace: "person", Value: person.ExternalIDs.InstagramID},
		{Provider: "twitter", Namespace: "person", Value: person.ExternalIDs.TwitterID},
		{Provider: "youtube", Namespace: "person", Value: person.ExternalIDs.YouTubeID},
		{Provider: "tiktok", Namespace: "person", Value: person.ExternalIDs.TikTokID},
	} {
		if strings.TrimSpace(claim.Value) != "" {
			claims = append(claims, claim)
		}
	}
	if err := persistPersonExternalEvidence(ctx, tx, entityID, "tmdb", payload.ObservationID, normalizedID, payload.ObservedAt, claims); err != nil {
		return err
	}

	gender := ""
	switch person.Gender {
	case 1:
		gender = "female"
	case 2:
		gender = "male"
	case 3:
		gender = "non_binary"
	}
	if _, err := tx.Exec(ctx, `UPDATE canonical_people SET display_name=$2,profile_image_id=COALESCE(NULLIF($3,'')::uuid,profile_image_id),biography=NULLIF($4,''),birth_date=NULLIF($5,'')::date,death_date=NULLIF($6,'')::date,gender=NULLIF($7,''),place_of_birth=NULLIF($8,''),known_for_department=NULLIF($9,''),homepage=NULLIF($10,''),popularity=NULLIF($11,0),updated_at=now() WHERE entity_id=$1`, entityID, person.Name, primaryImageID, person.Biography, person.Birthday, person.Deathday, gender, person.PlaceOfBirth, person.KnownForDepartment, person.Homepage, person.Popularity); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1 AND name_type IN('tmdb_primary','tmdb_alias')`, entityID)
	for index, name := range append([]string{person.Name}, person.AlsoKnownAs...) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		kind, quality := "tmdb_alias", 80
		if index == 0 {
			kind, quality = "tmdb_primary", 100
		}
		_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),$3,$4)ON CONFLICT DO NOTHING`, entityID, name, kind, quality)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM person_provider_credits WHERE person_entity_id=$1 AND provider='tmdb'`, entityID); err != nil {
		return err
	}
	for _, set := range []struct {
		creditType string
		credits    []tmdbPersonCredit
	}{{"cast", person.CombinedCredits.Cast}, {"crew", person.CombinedCredits.Crew}} {
		for _, credit := range set.credits {
			kind := ""
			switch credit.MediaType {
			case "movie":
				kind = "movie"
			case "tv":
				kind = "tv_show"
			default:
				continue
			}
			title := credit.Title
			date := credit.ReleaseDate
			if title == "" {
				title, date = credit.Name, credit.FirstAirDate
			}
			if credit.ID < 1 || strings.TrimSpace(title) == "" {
				continue
			}
			year := 0
			if len(date) >= 4 {
				year, _ = strconv.Atoi(date[:4])
			}
			imageID := ""
			if credit.PosterPath != "" {
				_ = tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,source_observation_id,ownership_scope)VALUES($1,'tmdb',$2,'poster',$3,$4,'person_credit')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,source_observation_id=EXCLUDED.source_observation_id,ownership_scope='person_credit' RETURNING id`, entityID, "credit:"+strconv.FormatInt(credit.ID, 10)+":"+credit.PosterPath, "https://image.tmdb.org/t/p/original"+credit.PosterPath, payload.ObservationID).Scan(&imageID)
			}
			_, err := tx.Exec(ctx, `INSERT INTO person_provider_credits(person_entity_id,provider,provider_target_id,media_kind,title,release_year,credit_type,character_name,department,job,credit_order,episode_count,image_id,source_observation_id,observed_at)VALUES($1,'tmdb',$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9,$10,$11,NULLIF($12,'')::uuid,$13,$14)ON CONFLICT(person_entity_id,provider,provider_target_id,credit_type,character_name,department,job)DO UPDATE SET title=EXCLUDED.title,release_year=EXCLUDED.release_year,credit_order=EXCLUDED.credit_order,episode_count=EXCLUDED.episode_count,image_id=EXCLUDED.image_id,source_observation_id=EXCLUDED.source_observation_id,observed_at=EXCLUDED.observed_at`, entityID, strconv.FormatInt(credit.ID, 10), kind, title, year, set.creditType, credit.Character, credit.Department, credit.Job, credit.Order, credit.EpisodeCount, imageID, payload.ObservationID, payload.ObservedAt)
			if err != nil {
				return err
			}
		}
	}

	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return err
	}
	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug FROM entities WHERE id=$1`, entityID).Scan(&slug); err != nil {
		return err
	}
	summary := map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "person", "slug": slug, "display": map[string]any{"title": person.Name, "image_id": primaryImageID}}
	summaryBody, _ := json.Marshal(summary)
	if _, err := tx.Exec(ctx, `UPDATE search_entities SET display_title=$2,summary=$3,projection_version=$4,popularity=NULLIF($5,0),updated_at=now() WHERE entity_id=$1`, entityID, person.Name, summaryBody, version, person.Popularity); err != nil {
		return err
	}
	freshUntil := time.Now().UTC().Add(30 * 24 * time.Hour)
	if person.Deathday != "" {
		freshUntil = time.Now().UTC().Add(180 * 24 * time.Hour)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at)VALUES($1,'tmdb',now(),now(),$2,NULLIF($3,0),$4)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at,failure_class=NULL,failure_message=NULL`, entityID, payload.ObservationID, jobID, freshUntil); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id)VALUES($1,'person',$2,'updated',$3,$4,$5)`, entityID, slug, []string{"identity", "detail", "search", "credits", "artwork"}, version, payload.ObservationID)
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = changelog.Sequence(ctx, s.runtime, 100)
	return s.proposeReconciliations(ctx, entityID)
}
