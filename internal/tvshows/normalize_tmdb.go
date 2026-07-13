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

const tmdbTVNormalizerVersion = "tmdb-tv-show/v5"

type tmdbImage struct {
	FilePath    string  `json:"file_path"`
	ISO6391     string  `json:"iso_639_1"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	VoteAverage float64 `json:"vote_average"`
}

type tmdbTV struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	Homepage         string  `json:"homepage"`
	FirstAirDate     string  `json:"first_air_date"`
	LastAirDate      string  `json:"last_air_date"`
	Status           string  `json:"status"`
	Type             string  `json:"type"`
	EpisodeRunTime   []int   `json:"episode_run_time"`
	NumberEpisodes   int     `json:"number_of_episodes"`
	NumberSeasons    int     `json:"number_of_seasons"`
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
		LogoPath      string `json:"logo_path"`
	} `json:"networks"`
	CreatedBy []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		ProfilePath string `json:"profile_path"`
	} `json:"created_by"`
	Companies []struct {
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
		ID            int64  `json:"id"`
		LogoPath      string `json:"logo_path"`
	} `json:"production_companies"`
	Seasons []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		AirDate      string `json:"air_date"`
		SeasonNumber int    `json:"season_number"`
		EpisodeCount int    `json:"episode_count"`
		Overview     string `json:"overview"`
		PosterPath   string `json:"poster_path"`
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
		Posters, Backdrops, Logos []tmdbImage
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
	Keywords struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	} `json:"keywords"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	Videos struct {
		Results []struct {
			Key      string `json:"key"`
			Name     string `json:"name"`
			Site     string `json:"site"`
			Type     string `json:"type"`
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Official bool   `json:"official"`
		} `json:"results"`
	} `json:"videos"`
	Recommendations struct {
		Results []struct {
			ID           int64   `json:"id"`
			Name         string  `json:"name"`
			OriginalName string  `json:"original_name"`
			FirstAirDate string  `json:"first_air_date"`
			PosterPath   string  `json:"poster_path"`
			Popularity   float64 `json:"popularity"`
		} `json:"results"`
	} `json:"recommendations"`
	Translations struct {
		Translations []struct {
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Data     struct {
				Name     string `json:"name"`
				Overview string `json:"overview"`
			} `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
}

type tmdbSeason struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	SeasonNumber int    `json:"season_number"`
	Images       struct {
		Posters []tmdbImage `json:"posters"`
	} `json:"images"`
	Episodes []struct {
		ID            int64   `json:"id"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		AirDate       string  `json:"air_date"`
		StillPath     string  `json:"still_path"`
		EpisodeNumber int     `json:"episode_number"`
		SeasonNumber  int     `json:"season_number"`
		Runtime       int     `json:"runtime"`
		VoteAverage   float64 `json:"vote_average"`
		VoteCount     int     `json:"vote_count"`
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
	seasonCount := value.NumberSeasons
	if seasonCount < 1 {
		for _, season := range value.Seasons {
			if season.SeasonNumber > 0 {
				seasonCount++
			}
		}
	}
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "tv_show", Provider: "tmdb", Namespace: "tv", ProviderID: strconv.FormatInt(value.ID, 10), PrimaryObservationID: payloads[0].ObservationID, ObservedAt: payloads[0].ObservedAt, NormalizerVersion: tmdbTVNormalizerVersion, Overview: value.Overview, Format: normalizeType(value.Type), Status: normalizeType(value.Status), Language: value.OriginalLanguage, Countries: value.Countries, StartDate: value.FirstAirDate, EndDate: value.LastAirDate, EpisodeCount: value.NumberEpisodes, SeasonCount: seasonCount, ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "tv", Value: strconv.FormatInt(value.ID, 10)}}}
	if value.Overview != "" {
		r.Overviews = append(r.Overviews, episodic.Text{Value: value.Overview, Type: "overview"})
	}
	if value.Homepage != "" {
		r.Links = append(r.Links, episodic.Link{Type: "homepage", URL: value.Homepage})
	}
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
	for _, creator := range value.CreatedBy {
		r.Credits = append(r.Credits, episodic.Credit{Provider: "tmdb", ProviderPersonID: strconv.FormatInt(creator.ID, 10), DisplayName: creator.Name, CreditType: "crew", Department: "Creator", Job: "Creator", ProfileURL: tmdbProfileURL(creator.ProfilePath)})
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
		logoURL := ""
		if item.LogoPath != "" {
			logoURL = "https://image.tmdb.org/t/p/original" + item.LogoPath
		}
		providerID := strconv.FormatInt(item.ID, 10)
		r.Networks = append(r.Networks, episodic.Network{Name: item.Name, Country: item.OriginCountry, Type: "network", ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "network", Value: providerID}}, LogoURL: logoURL, LogoProvider: "tmdb", LogoProviderID: "network:" + providerID + ":logo"})
	}
	for _, item := range value.Companies {
		r.Studios = append(r.Studios, item.Name)
		logoURL := ""
		if item.LogoPath != "" {
			logoURL = "https://image.tmdb.org/t/p/original" + item.LogoPath
		}
		providerID := strconv.FormatInt(item.ID, 10)
		r.Organizations = append(r.Organizations, episodic.Organization{Name: item.Name, Country: item.OriginCountry, Type: "production_company", ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "company", Value: providerID}}, LogoURL: logoURL, LogoProvider: "tmdb", LogoProviderID: "company:" + providerID + ":logo"})
	}
	for _, item := range value.AlternativeTitles.Results {
		r.Titles = append(r.Titles, episodic.Title{Value: item.Title, Country: item.ISO31661, Type: "alias"})
	}
	for _, item := range value.Seasons {
		// CollectTV deliberately omits TMDB season zero because it is commonly a
		// large bucket of clips and featurettes rather than canonical episodes.
		// Do not expose a season shell whose reported episode_count can never be
		// satisfied. A real specials season from TVDB/TVMaze is still merged in.
		if item.SeasonNumber == 0 {
			continue
		}
		providerID := strconv.FormatInt(item.ID, 10)
		season := episodic.Season{ProviderID: providerID, Number: item.SeasonNumber, Name: item.Name, Titles: []episodic.Title{{Value: item.Name, Type: "display"}}, EpisodeOrder: item.EpisodeCount, EpisodeCount: item.EpisodeCount, PremiereDate: item.AirDate, ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "season", Value: providerID}}}
		if item.Overview != "" {
			season.Overviews = []episodic.Text{{Value: item.Overview, Type: "overview"}}
		}
		if item.PosterPath != "" {
			season.Images = []episodic.Image{{Provider: "tmdb", ProviderID: "season:" + providerID + ":poster", URL: "https://image.tmdb.org/t/p/original" + item.PosterPath, Class: "poster"}}
		}
		r.Seasons = append(r.Seasons, season)
	}
	for _, translation := range value.Translations.Translations {
		if translation.Data.Name != "" {
			r.Titles = append(r.Titles, episodic.Title{Value: translation.Data.Name, Language: translation.Language, Country: translation.Country, Type: "translated"})
		}
		if translation.Data.Overview != "" {
			r.Overviews = append(r.Overviews, episodic.Text{Value: translation.Data.Overview, Language: translation.Language, Country: translation.Country, Type: "overview"})
		}
	}
	for _, keyword := range value.Keywords.Results {
		r.Keywords = append(r.Keywords, keyword.Name)
	}
	for _, rating := range value.ContentRatings.Results {
		if rating.Rating != "" {
			r.Certifications = append(r.Certifications, episodic.Certification{System: "tmdb", Country: rating.Country, Rating: rating.Rating})
		}
	}
	for _, video := range value.Videos.Results {
		if video.Key != "" {
			r.Videos = append(r.Videos, episodic.Video{Provider: strings.ToLower(video.Site), Type: normalizeType(video.Type), Name: video.Name, Key: video.Key, Language: video.Language, Country: video.Country, Official: video.Official})
		}
	}
	for _, recommendation := range value.Recommendations.Results[:min(len(value.Recommendations.Results), 50)] {
		id := strconv.FormatInt(recommendation.ID, 10)
		imageURL := ""
		if recommendation.PosterPath != "" {
			imageURL = "https://image.tmdb.org/t/p/original" + recommendation.PosterPath
		}
		r.Recommendations = append(r.Recommendations, episodic.Recommendation{Provider: "tmdb", ProviderID: id, Title: recommendation.Name, OriginalTitle: recommendation.OriginalName, FirstAirDate: recommendation.FirstAirDate, ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "tv", Value: id}}, ImageURL: imageURL, ProviderScore: recommendation.Popularity})
	}
	addTMDBImage := func(id, path, class, language string, width, height int, score float64) {
		if path != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tmdb", ProviderID: id, URL: "https://image.tmdb.org/t/p/original" + path, Class: class, Language: language, Width: width, Height: height, ProviderScore: score})
		}
	}
	addTMDBImage("poster", value.PosterPath, "poster", "", 0, 0, 0)
	addTMDBImage("backdrop", value.BackdropPath, "backdrop", "", 0, 0, 0)
	for _, item := range value.Images.Posters {
		addTMDBImage(item.FilePath, item.FilePath, "poster", item.ISO6391, item.Width, item.Height, item.VoteAverage)
	}
	for _, item := range value.Images.Backdrops {
		addTMDBImage(item.FilePath, item.FilePath, "backdrop", item.ISO6391, item.Width, item.Height, item.VoteAverage)
	}
	for _, item := range value.Images.Logos {
		addTMDBImage(item.FilePath, item.FilePath, "logo", item.ISO6391, item.Width, item.Height, item.VoteAverage)
	}
	for _, payload := range payloads[1:] {
		if payload.StatusCode != http.StatusOK {
			continue
		}
		var season tmdbSeason
		if json.Unmarshal(payload.Body, &season) != nil {
			continue
		}
		for i := range r.Seasons {
			if r.Seasons[i].Number != season.SeasonNumber {
				continue
			}
			if season.Overview != "" && len(r.Seasons[i].Overviews) == 0 {
				r.Seasons[i].Overviews = []episodic.Text{{Value: season.Overview, Type: "overview"}}
			}
			if season.PosterPath != "" {
				providerID := strconv.FormatInt(season.ID, 10)
				candidate := episodic.Image{Provider: "tmdb", ProviderID: "season:" + providerID + ":poster", URL: "https://image.tmdb.org/t/p/original" + season.PosterPath, Class: "poster"}
				if !containsTMDBSeasonImage(r.Seasons[i].Images, candidate) {
					r.Seasons[i].Images = append(r.Seasons[i].Images, candidate)
				}
			}
			for _, image := range season.Images.Posters {
				if image.FilePath == "" {
					continue
				}
				candidate := episodic.Image{Provider: "tmdb", ProviderID: "season:" + strconv.FormatInt(season.ID, 10) + ":" + image.FilePath, URL: "https://image.tmdb.org/t/p/original" + image.FilePath, Class: "poster", Language: image.ISO6391, Width: image.Width, Height: image.Height, ProviderScore: image.VoteAverage}
				if !containsTMDBSeasonImage(r.Seasons[i].Images, candidate) {
					r.Seasons[i].Images = append(r.Seasons[i].Images, candidate)
				}
			}
		}
		for _, episode := range season.Episodes {
			providerID := strconv.FormatInt(episode.ID, 10)
			item := episodic.Episode{ProviderID: providerID, ExternalIDs: []episodic.ExternalID{{Provider: "tmdb", Namespace: "episode", Value: providerID}}, Titles: []episodic.Title{{Value: episode.Name, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "aired", Season: episode.SeasonNumber, Number: float64(episode.EpisodeNumber), Provider: "tmdb"}, {Scheme: "tmdb", Season: episode.SeasonNumber, Number: float64(episode.EpisodeNumber), Provider: "tmdb"}}, IsSpecial: episode.SeasonNumber == 0, EpisodeType: "regular", AirDate: episode.AirDate, RuntimeMinutes: episode.Runtime, Summary: episode.Overview}
			if episode.SeasonNumber == 0 {
				item.EpisodeType = "special"
			}
			if episode.Overview != "" {
				item.Overviews = []episodic.Text{{Value: episode.Overview, Type: "overview"}}
			}
			if episode.VoteAverage > 0 {
				item.Ratings = []episodic.Rating{{System: "tmdb", Value: episode.VoteAverage, ScaleMin: 0, ScaleMax: 10, Votes: episode.VoteCount}}
			}
			if episode.StillPath != "" {
				item.Images = []episodic.Image{{Provider: "tmdb", ProviderID: "episode:" + providerID + ":still", URL: "https://image.tmdb.org/t/p/original" + episode.StillPath, Class: "still"}}
			}
			r.Episodes = append(r.Episodes, item)
		}
	}
	return r, nil
}

func containsTMDBSeasonImage(values []episodic.Image, candidate episodic.Image) bool {
	for _, value := range values {
		if value.Provider == candidate.Provider && value.URL == candidate.URL {
			return true
		}
	}
	return false
}
func tmdbProfileURL(path string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/original" + path
}
