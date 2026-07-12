package tvshows

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

const tmdbTVNormalizerVersion = "tmdb-tv-show/v1"

type tmdbTV struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	FirstAirDate     string  `json:"first_air_date"`
	LastAirDate      string  `json:"last_air_date"`
	Status           string  `json:"status"`
	Type             string  `json:"type"`
	EpisodeRunTime   []int   `json:"episode_run_time"`
	NumberEpisodes   int     `json:"number_of_episodes"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Genres           []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Countries []string `json:"origin_country"`
	Networks  []struct {
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
		ID            int64  `json:"id"`
	} `json:"networks"`
	Companies []struct {
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
		ID            int64  `json:"id"`
	} `json:"production_companies"`
	Seasons []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		AirDate      string `json:"air_date"`
		SeasonNumber int    `json:"season_number"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"seasons"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	ExternalIDs  struct {
		IMDbID string `json:"imdb_id"`
		TVDBID int64  `json:"tvdb_id"`
	} `json:"external_ids"`
	AlternativeTitles struct {
		Results []struct {
			Title    string `json:"title"`
			ISO31661 string `json:"iso_3166_1"`
		} `json:"results"`
	} `json:"alternative_titles"`
	Images struct {
		Posters, Backdrops []struct {
			FilePath string `json:"file_path"`
			ISO6391  string `json:"iso_639_1"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		}
	} `json:"images"`
	AggregateCredits struct {
		Cast []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Order       int    `json:"order"`
			ProfilePath string `json:"profile_path"`
			Roles       []struct {
				Character    string `json:"character"`
				EpisodeCount int    `json:"episode_count"`
			} `json:"roles"`
		} `json:"cast"`
		Crew []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Department  string `json:"department"`
			ProfilePath string `json:"profile_path"`
			Jobs        []struct {
				Job          string `json:"job"`
				EpisodeCount int    `json:"episode_count"`
			} `json:"jobs"`
		} `json:"crew"`
	} `json:"aggregate_credits"`
}

type tmdbSeason struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date"`
	SeasonNumber int    `json:"season_number"`
	Episodes     []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
		StillPath     string `json:"still_path"`
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Runtime       int    `json:"runtime"`
	} `json:"episodes"`
}

func normalizeTMDBTV(payloads []providers.Payload) (episodic.NormalizedRecord, error) {
	if len(payloads) == 0 || payloads[0].StatusCode != http.StatusOK {
		return episodic.NormalizedRecord{}, fmt.Errorf("TMDB returned no TV detail")
	}
	var value tmdbTV
	if err := json.Unmarshal(payloads[0].Body, &value); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	if value.ID < 1 || strings.TrimSpace(value.Name) == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid TMDB TV detail")
	}
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "tv_show", Provider: "tmdb", Namespace: "tv", ProviderID: strconv.FormatInt(value.ID, 10), PrimaryObservationID: payloads[0].ObservationID, ObservedAt: payloads[0].ObservedAt, NormalizerVersion: tmdbTVNormalizerVersion, Overview: value.Overview, Format: normalizeType(value.Type), Status: normalizeType(value.Status), Language: value.OriginalLanguage, Countries: value.Countries, StartDate: value.FirstAirDate, EndDate: value.LastAirDate, EpisodeCount: value.NumberEpisodes, ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "tv", Value: strconv.FormatInt(value.ID, 10)}}}
	r.Titles = append(r.Titles, episodic.Title{Value: value.Name, Type: "main"})
	if value.OriginalName != "" {
		r.Titles = append(r.Titles, episodic.Title{Value: value.OriginalName, Language: value.OriginalLanguage, Type: "original"})
	}
	if len(value.EpisodeRunTime) > 0 {
		r.RuntimeMinutes = value.EpisodeRunTime[0]
	}
	if value.VoteAverage > 0 {
		r.Ratings = append(r.Ratings, episodic.Rating{System: "tmdb", Value: value.VoteAverage, ScaleMin: 0, ScaleMax: 10, Votes: value.VoteCount})
	}
	for _, cast := range value.AggregateCredits.Cast {
		character := ""
		if len(cast.Roles) > 0 {
			character = cast.Roles[0].Character
		}
		r.Credits = append(r.Credits, episodic.Credit{Provider: "tmdb", ProviderPersonID: strconv.FormatInt(cast.ID, 10), DisplayName: cast.Name, CreditType: "cast", Character: character, Order: cast.Order, ProfileURL: tmdbProfileURL(cast.ProfilePath)})
	}
	for _, crew := range value.AggregateCredits.Crew {
		for _, job := range crew.Jobs {
			r.Credits = append(r.Credits, episodic.Credit{Provider: "tmdb", ProviderPersonID: strconv.FormatInt(crew.ID, 10), DisplayName: crew.Name, CreditType: "crew", Department: crew.Department, Job: job.Job, ProfileURL: tmdbProfileURL(crew.ProfilePath)})
		}
	}
	if value.ExternalIDs.IMDbID != "" {
		r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: value.ExternalIDs.IMDbID})
	}
	if value.ExternalIDs.TVDBID > 0 {
		r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(value.ExternalIDs.TVDBID, 10)})
	}
	for _, item := range value.Genres {
		r.Genres = append(r.Genres, item.Name)
	}
	for _, item := range value.Networks {
		r.Networks = append(r.Networks, episodic.Network{Name: item.Name, Country: item.OriginCountry, Type: "network"})
	}
	for _, item := range value.Companies {
		r.Studios = append(r.Studios, item.Name)
	}
	for _, item := range value.AlternativeTitles.Results {
		r.Titles = append(r.Titles, episodic.Title{Value: item.Title, Country: item.ISO31661, Type: "alias"})
	}
	for _, item := range value.Seasons {
		r.Seasons = append(r.Seasons, episodic.Season{ProviderID: strconv.FormatInt(item.ID, 10), Number: item.SeasonNumber, Name: item.Name, EpisodeOrder: item.EpisodeCount, PremiereDate: item.AirDate})
	}
	addTMDBImage := func(id, path, class string, width, height int) {
		if path != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tmdb", ProviderID: id, URL: "https://image.tmdb.org/t/p/original" + path, Class: class, Width: width, Height: height})
		}
	}
	addTMDBImage("poster", value.PosterPath, "poster", 0, 0)
	addTMDBImage("backdrop", value.BackdropPath, "backdrop", 0, 0)
	for _, item := range value.Images.Posters[:min(len(value.Images.Posters), 25)] {
		addTMDBImage(item.FilePath, item.FilePath, "poster", item.Width, item.Height)
	}
	for _, item := range value.Images.Backdrops[:min(len(value.Images.Backdrops), 25)] {
		addTMDBImage(item.FilePath, item.FilePath, "backdrop", item.Width, item.Height)
	}
	for _, payload := range payloads[1:] {
		if payload.StatusCode != http.StatusOK {
			continue
		}
		var season tmdbSeason
		if json.Unmarshal(payload.Body, &season) != nil {
			continue
		}
		for _, episode := range season.Episodes {
			r.Episodes = append(r.Episodes, episodic.Episode{ProviderID: strconv.FormatInt(episode.ID, 10), Titles: []episodic.Title{{Value: episode.Name, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "tmdb", Season: episode.SeasonNumber, Number: float64(episode.EpisodeNumber)}}, AirDate: episode.AirDate, RuntimeMinutes: episode.Runtime, Summary: episode.Overview})
			addTMDBImage("episode:"+strconv.FormatInt(episode.ID, 10), episode.StillPath, "still", 0, 0)
		}
	}
	return r, nil
}
func tmdbProfileURL(path string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/original" + path
}
