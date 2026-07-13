package people

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
)

type supplementalPerson struct {
	Provider, ID, Name, Biography, BirthDate, DeathDate, Gender, PlaceOfBirth string
	Aliases, Profiles                                                         []string
	Biographies                                                               map[string]string
	Credits                                                                   []supplementalCredit
}

type supplementalCredit struct {
	ProviderTargetID, Kind, Title, CreditType, Character, Department, Job, ImageURL string
	Year, Order, EpisodeCount                                                       int
}

type tvmazePerson struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Birthday string `json:"birthday"`
	Deathday string `json:"deathday"`
	Gender   string `json:"gender"`
	Country  *struct {
		Name string `json:"name"`
	} `json:"country"`
	Image *struct{ Medium, Original string } `json:"image"`
}

type tvmazeCredit struct {
	Type  string `json:"type"`
	Links struct {
		Show struct {
			Href string `json:"href"`
		} `json:"show"`
		Character struct {
			Name string `json:"name"`
		} `json:"character"`
	} `json:"_links"`
	Embedded struct {
		Show struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			Premiered string `json:"premiered"`
			Image     *struct {
				Medium   string `json:"medium"`
				Original string `json:"original"`
			} `json:"image"`
		} `json:"show"`
	} `json:"_embedded"`
}

type tvdbPersonEnvelope struct {
	Data struct {
		ID         int64  `json:"id"`
		Gender     int64  `json:"gender"`
		Name       string `json:"name"`
		Birth      string `json:"birth"`
		Death      string `json:"death"`
		BirthPlace string `json:"birthPlace"`
		Image      string `json:"image"`
		Aliases    []struct {
			Name string `json:"name"`
		} `json:"aliases"`
		Biographies []struct {
			Language  string `json:"language"`
			Biography string `json:"biography"`
		} `json:"biographies"`
		Characters []struct {
			Name       string `json:"name"`
			PeopleType string `json:"peopleType"`
			SeriesID   int64  `json:"seriesId"`
			MovieID    int64  `json:"movieId"`
			Series     *struct {
				Name  string `json:"name"`
				Image string `json:"image"`
				Year  string `json:"year"`
			} `json:"series"`
			Movie *struct {
				Name  string `json:"name"`
				Image string `json:"image"`
				Year  string `json:"year"`
			} `json:"movie"`
		} `json:"characters"`
	} `json:"data"`
}

func (s *Service) EnrichTVMaze(ctx context.Context, entityID, providerID string, jobID int64) error {
	capability := tvmaze.New(s.runtime.Config.Providers.TVMaze).Capability()
	cache, err := providercache.New(s.runtime, "tvmaze-person/v1", capability.RawRetention, capability.ResponseCache, jobID)
	if err != nil {
		return err
	}
	payloads, err := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, cache).Collect(ctx, providers.Identifier{Provider: "tvmaze", Namespace: "person", Value: providerID})
	if err != nil {
		return err
	}
	if len(payloads) == 0 {
		return fmt.Errorf("TVMaze returned no person detail")
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
	}
	var value tvmazePerson
	if err := json.Unmarshal(payload.Body, &value); err != nil {
		return err
	}
	if value.ID < 1 || strings.TrimSpace(value.Name) == "" {
		return fmt.Errorf("invalid TVMaze person detail")
	}
	person := supplementalPerson{Provider: "tvmaze", ID: strconv.FormatInt(value.ID, 10), Name: value.Name, BirthDate: value.Birthday, DeathDate: value.Deathday, Gender: strings.ToLower(value.Gender), Biographies: map[string]string{}}
	if value.Country != nil {
		person.PlaceOfBirth = value.Country.Name
	}
	if value.Image != nil {
		person.Profiles = append(person.Profiles, value.Image.Original, value.Image.Medium)
	}
	for index, creditsPayload := range payloads[1:] {
		if creditsPayload.StatusCode != http.StatusOK {
			continue
		}
		var credits []tvmazeCredit
		if err := json.Unmarshal(creditsPayload.Body, &credits); err != nil {
			return err
		}
		for _, credit := range credits {
			id := strconv.FormatInt(credit.Embedded.Show.ID, 10)
			if credit.Embedded.Show.ID < 1 {
				id = linkedID(credit.Links.Show.Href)
			}
			if id == "" || credit.Embedded.Show.Name == "" {
				continue
			}
			item := supplementalCredit{ProviderTargetID: id, Kind: "tv_show", Title: credit.Embedded.Show.Name, Year: yearValue(credit.Embedded.Show.Premiered)}
			if credit.Embedded.Show.Image != nil {
				item.ImageURL = credit.Embedded.Show.Image.Original
				if item.ImageURL == "" {
					item.ImageURL = credit.Embedded.Show.Image.Medium
				}
			}
			if index == 0 {
				item.CreditType = "cast"
				item.Character = credit.Links.Character.Name
			} else {
				item.CreditType = "crew"
				item.Job = credit.Type
			}
			person.Credits = append(person.Credits, item)
		}
	}
	return s.persistSupplemental(ctx, entityID, payload, person, jobID)
}

func (s *Service) EnrichTVDB(ctx context.Context, entityID, providerID string, jobID int64, credentials providercredentials.Credentials) error {
	capability := tvdb.PersonCapability()
	cache, err := providercache.New(s.runtime, "tvdb-person/v1", capability.RawRetention, capability.ResponseCache, jobID)
	if err != nil {
		return err
	}
	payloads, err := tvdb.NewCached(s.runtime.Config.Providers.TVDB, cache, credentials.APIKey("tvdb"), s.runtime.Redis).CollectPerson(ctx, providers.Identifier{Provider: "tvdb", Namespace: "person", Value: providerID})
	if err != nil {
		return err
	}
	if len(payloads) == 0 {
		return fmt.Errorf("TVDB returned no person detail")
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return &providers.StatusError{Provider: "tvdb", StatusCode: payload.StatusCode}
	}
	var value tvdbPersonEnvelope
	if err := json.Unmarshal(payload.Body, &value); err != nil {
		return err
	}
	if value.Data.ID < 1 || strings.TrimSpace(value.Data.Name) == "" {
		return fmt.Errorf("invalid TVDB person detail")
	}
	gender := ""
	switch value.Data.Gender {
	case 1:
		gender = "male"
	case 2:
		gender = "female"
	case 3:
		gender = "non_binary"
	}
	person := supplementalPerson{Provider: "tvdb", ID: strconv.FormatInt(value.Data.ID, 10), Name: value.Data.Name, BirthDate: value.Data.Birth, DeathDate: value.Data.Death, Gender: gender, PlaceOfBirth: value.Data.BirthPlace, Aliases: []string{}, Biographies: map[string]string{}}
	if image := tvdbImageURL(value.Data.Image); image != "" {
		person.Profiles = append(person.Profiles, image)
	}
	for _, alias := range value.Data.Aliases {
		person.Aliases = append(person.Aliases, alias.Name)
	}
	for _, bio := range value.Data.Biographies {
		if bio.Biography != "" {
			person.Biographies[bio.Language] = bio.Biography
			if person.Biography == "" && (bio.Language == "eng" || bio.Language == "en") {
				person.Biography = bio.Biography
			}
		}
	}
	for _, credit := range value.Data.Characters {
		item := supplementalCredit{CreditType: "crew", Character: credit.Name, Job: credit.PeopleType}
		if strings.EqualFold(credit.PeopleType, "Actor") || strings.EqualFold(credit.PeopleType, "Guest Star") {
			item.CreditType = "cast"
			item.Job = ""
		}
		if credit.Series != nil && credit.SeriesID > 0 {
			item.ProviderTargetID = strconv.FormatInt(credit.SeriesID, 10)
			item.Kind = "tv_show"
			item.Title = credit.Series.Name
			item.ImageURL = tvdbImageURL(credit.Series.Image)
			item.Year = yearValue(credit.Series.Year)
		} else if credit.Movie != nil && credit.MovieID > 0 {
			item.ProviderTargetID = strconv.FormatInt(credit.MovieID, 10)
			item.Kind = "movie"
			item.Title = credit.Movie.Name
			item.ImageURL = tvdbImageURL(credit.Movie.Image)
			item.Year = yearValue(credit.Movie.Year)
		}
		if item.ProviderTargetID != "" && item.Title != "" {
			person.Credits = append(person.Credits, item)
		}
	}
	return s.persistSupplemental(ctx, entityID, payload, person, jobID)
}

func (s *Service) persistSupplemental(ctx context.Context, entityID string, payload providers.Payload, person supplementalPerson, jobID int64) error {
	var claimed bool
	if err := s.runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND provider=$2 AND namespace='person' AND normalized_value=$3 AND state='accepted')`, entityID, person.Provider, person.ID).Scan(&claimed); err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("%s person %s is not claimed by canonical person %s", person.Provider, person.ID, entityID)
	}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	body, _ := json.Marshal(person)
	if _, err := tx.Exec(ctx, `INSERT INTO normalized_records(entity_id,entity_kind,provider,provider_namespace,provider_record_id,primary_observation_id,normalizer_version,schema_version,document,observed_at)VALUES($1,'person',$2,'person',$3,$4,$5,1,$6,$7)ON CONFLICT(primary_observation_id,normalizer_version,schema_version)DO UPDATE SET document=EXCLUDED.document,entity_id=EXCLUDED.entity_id`, entityID, person.Provider, person.ID, payload.ObservationID, person.Provider+"-person/v1", body, payload.ObservedAt); err != nil {
		return err
	}
	primaryImageID := ""
	seenImages := map[string]bool{}
	for _, source := range person.Profiles {
		if source == "" || seenImages[source] {
			continue
		}
		seenImages[source] = true
		var imageID string
		if err := tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,source_observation_id,ownership_scope)VALUES($1,$2,$3,'profile',$3,$4,'person_profile')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, person.Provider, source, payload.ObservationID).Scan(&imageID); err != nil {
			return err
		}
		if primaryImageID == "" {
			primaryImageID = imageID
		}
	}
	biographies, _ := json.Marshal(person.Biographies)
	birthDate := validDate(person.BirthDate)
	deathDate := validDate(person.DeathDate)
	if _, err := tx.Exec(ctx, `UPDATE canonical_people SET display_name=CASE WHEN display_name='' THEN $2 ELSE display_name END,profile_image_id=COALESCE(profile_image_id,NULLIF($3,'')::uuid),biography=COALESCE(biography,NULLIF($4,'')),birth_date=COALESCE(birth_date,NULLIF($5,'')::date),death_date=COALESCE(death_date,NULLIF($6,'')::date),gender=COALESCE(gender,NULLIF($7,'')),place_of_birth=COALESCE(place_of_birth,NULLIF($8,'')),biographies=biographies||$9::jsonb,updated_at=now() WHERE entity_id=$1`, entityID, person.Name, primaryImageID, person.Biography, birthDate, deathDate, person.Gender, person.PlaceOfBirth, biographies); err != nil {
		return err
	}
	for index, name := range append([]string{person.Name}, person.Aliases...) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		kind, quality := person.Provider+"_alias", 75
		if index == 0 {
			kind, quality = person.Provider+"_primary", 90
		}
		_, _ = tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)VALUES($1,$2,lower(unaccent($2)),$3,$4)ON CONFLICT DO NOTHING`, entityID, name, kind, quality)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM person_provider_credits WHERE person_entity_id=$1 AND provider=$2`, entityID, person.Provider); err != nil {
		return err
	}
	for _, credit := range person.Credits {
		imageID := ""
		if credit.ImageURL != "" {
			_ = tx.QueryRow(ctx, `INSERT INTO image_candidates(entity_id,provider,provider_image_id,class,source_url,source_observation_id,ownership_scope)VALUES($1,$2,$3,'poster',$4,$5,'person_credit')ON CONFLICT(entity_id,provider,provider_image_id,class)DO UPDATE SET source_url=EXCLUDED.source_url,source_observation_id=EXCLUDED.source_observation_id RETURNING id`, entityID, person.Provider, "credit:"+credit.ProviderTargetID+":"+credit.ImageURL, credit.ImageURL, payload.ObservationID).Scan(&imageID)
		}
		_, err := tx.Exec(ctx, `INSERT INTO person_provider_credits(person_entity_id,provider,provider_target_id,media_kind,title,release_year,credit_type,character_name,department,job,credit_order,episode_count,image_id,source_observation_id,observed_at)VALUES($1,$2,$3,$4,$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,NULLIF($13,'')::uuid,$14,$15)ON CONFLICT(person_entity_id,provider,provider_target_id,credit_type,character_name,department,job)DO UPDATE SET title=EXCLUDED.title,release_year=EXCLUDED.release_year,image_id=EXCLUDED.image_id,source_observation_id=EXCLUDED.source_observation_id,observed_at=EXCLUDED.observed_at`, entityID, person.Provider, credit.ProviderTargetID, credit.Kind, credit.Title, credit.Year, credit.CreditType, credit.Character, credit.Department, credit.Job, credit.Order, credit.EpisodeCount, imageID, payload.ObservationID, payload.ObservedAt)
		if err != nil {
			return err
		}
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version`, entityID).Scan(&version); err != nil {
		return err
	}
	var displayName, displayImage, slug string
	if err := tx.QueryRow(ctx, `SELECT p.display_name,COALESCE(p.profile_image_id::text,''),e.slug FROM canonical_people p JOIN entities e ON e.id=p.entity_id WHERE p.entity_id=$1`, entityID).Scan(&displayName, &displayImage, &slug); err != nil {
		return err
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "person", "slug": slug, "display": map[string]any{"title": displayName, "image_id": displayImage}})
	if _, err := tx.Exec(ctx, `UPDATE search_entities SET display_title=$2,summary=$3,projection_version=$4,updated_at=now() WHERE entity_id=$1`, entityID, displayName, summary, version); err != nil {
		return err
	}
	freshUntil := time.Now().UTC().Add(30 * 24 * time.Hour)
	if deathDate != "" {
		freshUntil = time.Now().UTC().Add(180 * 24 * time.Hour)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,current_job_id,next_eligible_at)VALUES($1,$2,now(),now(),$3,NULLIF($4,0),$5)ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),last_success_at=now(),last_observation_id=EXCLUDED.last_observation_id,current_job_id=EXCLUDED.current_job_id,next_eligible_at=EXCLUDED.next_eligible_at,failure_class=NULL,failure_message=NULL`, entityID, person.Provider, payload.ObservationID, jobID, freshUntil); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,provider_observation_id)VALUES($1,'person',$2,'updated',$3,$4,$5)`, entityID, slug, []string{"detail", "search", "credits", "artwork"}, version, payload.ObservationID)
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = changelog.Sequence(ctx, s.runtime, 100)
	return s.proposeReconciliations(ctx, entityID)
}

func linkedID(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	value := path.Base(strings.TrimRight(parsed.Path, "/"))
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return ""
	}
	return value
}
func tvdbImageURL(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://artworks.thetvdb.com/" + strings.TrimLeft(value, "/")
}
func validDate(value string) string {
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return value
	}
	return ""
}
func yearValue(value string) int {
	if len(value) >= 4 {
		year, _ := strconv.Atoi(value[:4])
		return year
	}
	return 0
}
