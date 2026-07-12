package anime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

// This intentionally keeps only the mapped TVDB season. A TVDB series often
// represents an entire franchise while one AniDB AID represents one cour.
func normalizeTVDBAnime(payload providers.Payload, season, offset int) (episodic.NormalizedRecord, error) {
	var wrapper struct {
		Data struct {
			ID          int64 `json:"id"`
			Name, Image string
			Episodes    []struct {
				ID                            int64 `json:"id"`
				Name, Overview, Aired         string
				SeasonNumber, Number, Runtime int
			} `json:"episodes"`
			Artworks []struct {
				ID                  int64 `json:"id"`
				Image               string
				Type, Width, Height int
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
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "tvdb", Namespace: "series", ProviderID: strconv.FormatInt(v.ID, 10), PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, NormalizerVersion: "tvdb-anime-series/v1", ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(v.ID, 10)}}}
	if v.Name != "" {
		r.Titles = []episodic.Title{{Value: v.Name, Type: "alias"}}
	}
	if v.Image != "" {
		r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: "primary", URL: animeTVDBURL(v.Image), Class: "poster"})
	}
	for _, x := range v.Artworks[:min(len(v.Artworks), 50)] {
		class := map[int]string{1: "banner", 2: "poster", 3: "backdrop", 6: "poster", 7: "backdrop", 13: "banner", 14: "poster", 15: "backdrop", 25: "logo"}[x.Type]
		if class != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(x.ID, 10), URL: animeTVDBURL(x.Image), Class: class, Width: x.Width, Height: x.Height})
		}
	}
	for _, x := range v.Episodes {
		if x.SeasonNumber != season || x.Number <= offset {
			continue
		}
		r.Episodes = append(r.Episodes, episodic.Episode{ProviderID: strconv.FormatInt(x.ID, 10), Titles: []episodic.Title{{Value: x.Name, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "tvdb", Season: season, Number: float64(x.Number - offset)}}, AirDate: x.Aired, RuntimeMinutes: x.Runtime, Summary: x.Overview})
		last := &r.Episodes[len(r.Episodes)-1]
		last.Numbers[0].Number = float64(x.Number)
		last.Numbers = append(last.Numbers, episodic.EpisodeNumber{Scheme: "aired", Number: float64(x.Number - offset)})
	}
	return r, nil
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
