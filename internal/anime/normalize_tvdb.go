package anime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
)

// This intentionally keeps only the mapped TVDB season. A TVDB series often
// represents an entire franchise while one AniDB AID represents one cour.
func normalizeTVDBAnime(payload providers.Payload, season, offset int, seasonPayloads ...providers.Payload) (episodic.NormalizedRecord, error) {
	var wrapper struct {
		Data struct {
			ID                            int64 `json:"id"`
			Name, Image, OriginalLanguage string
			Seasons                       []struct {
				ID     int64  `json:"id"`
				Number int    `json:"number"`
				Name   string `json:"name"`
				Image  string `json:"image"`
			} `json:"seasons"`
			Episodes []struct {
				ID                                            int64 `json:"id"`
				Name, Overview, Aired, Image                  string
				SeasonNumber, Number, AbsoluteNumber, Runtime int
				Translations                                  struct {
					Names []struct {
						Language, Name string
					} `json:"nameTranslations"`
					Overviews []struct {
						Language, Overview string
					} `json:"overviewTranslations"`
				} `json:"translations"`
			} `json:"episodes"`
			Artworks []struct {
				ID                  int64 `json:"id"`
				Image, Language     string
				Type, Width, Height int
				Score               float64
			} `json:"artworks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload.Body, &wrapper); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	v := wrapper.Data
	if v.ID < 1 {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid TVDB anime supplement")
	}
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "tvdb", Namespace: "series", ProviderID: strconv.FormatInt(v.ID, 10), PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, NormalizerVersion: fmt.Sprintf("%s/season/%d", tvdbAnimeNormalizerVersion, season), ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(v.ID, 10)}}}
	if v.Name != "" {
		r.Titles = []episodic.Title{{Value: v.Name, Type: "alias"}}
	}
	if v.Image != "" {
		r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: "primary", URL: animeTVDBURL(v.Image), Class: "poster", Language: v.OriginalLanguage})
	}
	for _, x := range v.Artworks {
		class := tvdb.ArtworkClass(x.Type)
		if class != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(x.ID, 10), URL: animeTVDBURL(x.Image), Class: class, Language: x.Language, Width: x.Width, Height: x.Height, ProviderScore: x.Score})
		}
	}
	for _, sourceSeason := range v.Seasons {
		if sourceSeason.Number != season {
			continue
		}
		providerID := strconv.FormatInt(sourceSeason.ID, 10)
		if season != 1 {
			r.Namespace = "season"
			r.ProviderID = providerID
			r.ExternalIDs = []episodic.ExternalID{{Provider: "tvdb", Namespace: "season", Value: providerID}}
		}
		item := episodic.Season{ProviderID: providerID, Number: 1, Name: sourceSeason.Name, ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "season", Value: providerID}}}
		if item.Name == "" {
			item.Name = "Season 1"
		}
		item.Titles = []episodic.Title{{Value: item.Name, Language: v.OriginalLanguage, Type: "display"}}
		if sourceSeason.Image != "" {
			item.Images = []episodic.Image{{Provider: "tvdb", ProviderID: "season:" + providerID + ":poster", URL: animeTVDBURL(sourceSeason.Image), Class: "poster", Language: v.OriginalLanguage}}
		}
		r.Seasons = append(r.Seasons, item)
		break
	}
	if len(r.Seasons) == 0 {
		r.Seasons = []episodic.Season{{Number: 1, Name: "Season 1", Titles: []episodic.Title{{Value: "Season 1", Language: "en", Type: "display"}}}}
		if season != 1 {
			r.Namespace = "series_season"
			r.ProviderID = fmt.Sprintf("%d:%d", v.ID, season)
			r.ExternalIDs = nil
		}
	}
	for _, seasonPayload := range seasonPayloads {
		appendTVDBAnimeSeasonArtwork(&r, seasonPayload, season)
	}
	for _, x := range v.Episodes {
		if x.SeasonNumber != season || x.Number <= offset {
			continue
		}
		providerID := strconv.FormatInt(x.ID, 10)
		relativeNumber := x.Number - offset
		item := episodic.Episode{ProviderID: providerID, ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "episode", Value: providerID}}, Titles: []episodic.Title{{Value: x.Name, Language: v.OriginalLanguage, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "aired", Season: 1, Number: float64(relativeNumber), Provider: "tvdb"}, {Scheme: "tvdb", Season: season, Number: float64(x.Number), Provider: "tvdb"}}, EpisodeType: "regular", AirDate: x.Aired, RuntimeMinutes: x.Runtime, Summary: strings.TrimSpace(x.Overview)}
		if x.AbsoluteNumber > 0 {
			item.Numbers = append(item.Numbers, episodic.EpisodeNumber{Scheme: "absolute", Number: float64(x.AbsoluteNumber), Provider: "tvdb"})
		}
		if item.Summary != "" {
			item.Overviews = append(item.Overviews, episodic.Text{Value: item.Summary, Language: v.OriginalLanguage, Type: "overview"})
		}
		for _, translation := range x.Translations.Names {
			if strings.TrimSpace(translation.Name) != "" {
				item.Titles = append(item.Titles, episodic.Title{Value: strings.TrimSpace(translation.Name), Language: translation.Language, Type: "translated"})
			}
		}
		for _, translation := range x.Translations.Overviews {
			if strings.TrimSpace(translation.Overview) != "" {
				item.Overviews = append(item.Overviews, episodic.Text{Value: strings.TrimSpace(translation.Overview), Language: translation.Language, Type: "overview"})
			}
		}
		if x.Image != "" {
			item.Images = []episodic.Image{{Provider: "tvdb", ProviderID: "episode:" + providerID + ":still", URL: animeTVDBURL(x.Image), Class: "still"}}
		}
		r.Episodes = append(r.Episodes, item)
	}
	r.EpisodeCount = len(r.Episodes)
	r.SeasonCount = len(r.Seasons)
	return r, nil
}

func appendTVDBAnimeSeasonArtwork(record *episodic.NormalizedRecord, payload providers.Payload, sourceSeason int) {
	var wrapper struct {
		Data struct {
			Number  int `json:"number"`
			Artwork []struct {
				ID                  int64 `json:"id"`
				Image, Language     string
				Type, Width, Height int
				Score               float64
			} `json:"artwork"`
		} `json:"data"`
	}
	if json.Unmarshal(payload.Body, &wrapper) != nil || wrapper.Data.Number != sourceSeason || len(record.Seasons) == 0 {
		return
	}
	for _, artwork := range wrapper.Data.Artwork {
		class := tvdb.ArtworkClass(artwork.Type)
		url := animeTVDBURL(artwork.Image)
		if class == "" || artwork.ID < 1 || url == "" {
			continue
		}
		candidate := episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(artwork.ID, 10), URL: url, Class: class, Language: artwork.Language, Width: artwork.Width, Height: artwork.Height, ProviderScore: artwork.Score}
		duplicate := false
		for _, existing := range record.Seasons[0].Images {
			if existing.Provider == candidate.Provider && existing.URL == candidate.URL {
				duplicate = true
				break
			}
		}
		if !duplicate {
			record.Seasons[0].Images = append(record.Seasons[0].Images, candidate)
		}
	}
}
func animeTVDBURL(value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if value == "" {
		return ""
	}
	return "https://artworks.thetvdb.com/" + strings.TrimLeft(value, "/")
}
