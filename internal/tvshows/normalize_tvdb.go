package tvshows

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

const tvdbSeriesNormalizerVersion = "tvdb-series/v1"

type tvdbEnvelope struct {
	Data tvdbSeries `json:"data"`
}
type tvdbSeries struct {
	ID                                                                    int64 `json:"id"`
	Name, FirstAired, LastAired, Image, OriginalCountry, OriginalLanguage string
	AverageRuntime                                                        int `json:"averageRuntime"`
	Status                                                                struct {
		Name string `json:"name"`
	} `json:"status"`
	Aliases         []struct{ Language, Name string } `json:"aliases"`
	Genres, Studios []struct {
		Name string `json:"name"`
	}
	RemoteIDs []struct {
		ID         string `json:"id"`
		Type       int    `json:"type"`
		SourceName string `json:"sourceName"`
	} `json:"remoteIds"`
	Artworks []struct {
		ID                  int64 `json:"id"`
		Image, Language     string
		Type, Width, Height int
	} `json:"artworks"`
	Seasons []struct {
		ID     int64  `json:"id"`
		Number int    `json:"number"`
		Name   string `json:"name"`
	} `json:"seasons"`
	Episodes []struct {
		ID                                            int64 `json:"id"`
		Name, Overview, Aired                         string
		SeasonNumber, Number, AbsoluteNumber, Runtime int
	} `json:"episodes"`
	Characters []struct {
		ID             int64  `json:"id"`
		PeopleID       int64  `json:"peopleId"`
		PersonName     string `json:"personName"`
		PeopleType     string `json:"peopleType"`
		Name           string `json:"name"`
		Sort           int    `json:"sort"`
		PersonImageURL string `json:"personImgURL"`
	} `json:"characters"`
	Score float64 `json:"score"`
}

func normalizeTVDBSeries(payload providers.Payload, kind string, seasonFilter *int, episodeOffset int) (episodic.NormalizedRecord, error) {
	var wrapper tvdbEnvelope
	if err := json.Unmarshal(payload.Body, &wrapper); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	v := wrapper.Data
	if v.ID < 1 || strings.TrimSpace(v.Name) == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid TVDB series detail")
	}
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: kind, Provider: "tvdb", Namespace: "series", ProviderID: strconv.FormatInt(v.ID, 10), PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, NormalizerVersion: tvdbSeriesNormalizerVersion, Titles: []episodic.Title{{Value: v.Name, Type: "main"}}, Status: normalizeType(v.Status.Name), Language: v.OriginalLanguage, Countries: []string{v.OriginalCountry}, StartDate: v.FirstAired, EndDate: v.LastAired, RuntimeMinutes: v.AverageRuntime, ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(v.ID, 10)}}}
	if v.Score > 0 && v.Score <= 10 {
		r.Ratings = append(r.Ratings, episodic.Rating{System: "tvdb", Value: v.Score, ScaleMin: 0, ScaleMax: 10})
	}
	for _, character := range v.Characters {
		creditType := "crew"
		if strings.EqualFold(character.PeopleType, "actor") {
			creditType = "cast"
		}
		personID := character.PeopleID
		if personID == 0 {
			personID = character.ID
		}
		r.Credits = append(r.Credits, episodic.Credit{Provider: "tvdb", ProviderPersonID: strconv.FormatInt(personID, 10), DisplayName: character.PersonName, CreditType: creditType, Character: character.Name, Job: character.PeopleType, Order: character.Sort, ProfileURL: tvdbArtworkURL(character.PersonImageURL)})
	}
	for _, x := range v.Aliases {
		r.Titles = append(r.Titles, episodic.Title{Value: x.Name, Language: x.Language, Type: "alias"})
	}
	for _, x := range v.Genres {
		r.Genres = append(r.Genres, x.Name)
	}
	for _, x := range v.Studios {
		r.Studios = append(r.Studios, x.Name)
	}
	for _, x := range v.RemoteIDs {
		switch x.Type {
		case 2:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: x.ID})
		case 12:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "tmdb", Namespace: "tv", Value: x.ID})
		case 18:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "wikidata", Namespace: "item", Value: strings.ToUpper(x.ID)})
		case 23:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "anidb", Namespace: "anime", Value: x.ID})
		}
	}
	if v.Image != "" {
		r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: "primary", URL: tvdbArtworkURL(v.Image), Class: "poster"})
	}
	for _, x := range v.Artworks[:min(len(v.Artworks), 50)] {
		class := map[int]string{1: "banner", 2: "poster", 3: "backdrop", 6: "poster", 7: "backdrop", 13: "banner", 14: "poster", 15: "backdrop", 25: "logo"}[x.Type]
		if class != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(x.ID, 10), URL: tvdbArtworkURL(x.Image), Class: class, Width: x.Width, Height: x.Height})
		}
	}
	for _, x := range v.Seasons {
		if seasonFilter == nil && x.Number <= 0 {
			continue
		}
		if seasonFilter == nil || x.Number == *seasonFilter {
			r.Seasons = append(r.Seasons, episodic.Season{ProviderID: strconv.FormatInt(x.ID, 10), Number: x.Number, Name: x.Name})
		}
	}
	for _, x := range v.Episodes {
		if seasonFilter == nil && x.SeasonNumber <= 0 {
			continue
		}
		if seasonFilter != nil && x.SeasonNumber != *seasonFilter {
			continue
		}
		number := x.Number - episodeOffset
		if number < 1 {
			continue
		}
		r.Episodes = append(r.Episodes, episodic.Episode{ProviderID: strconv.FormatInt(x.ID, 10), Titles: []episodic.Title{{Value: x.Name, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "tvdb", Season: x.SeasonNumber, Number: float64(number)}}, AirDate: x.Aired, RuntimeMinutes: x.Runtime, Summary: x.Overview})
	}
	r.EpisodeCount = len(r.Episodes)
	return r, nil
}

func tvdbArtworkURL(value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if value == "" {
		return ""
	}
	return "https://artworks.thetvdb.com/" + strings.TrimLeft(value, "/")
}
