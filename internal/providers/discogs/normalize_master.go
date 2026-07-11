package discogs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func NormalizeMaster(body []byte, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var source struct {
		ID          int64    `json:"id"`
		Title       string   `json:"title"`
		Year        int      `json:"year"`
		MainRelease int64    `json:"main_release"`
		URI         string   `json:"uri"`
		Genres      []string `json:"genres"`
		Styles      []string `json:"styles"`
		NumForSale  int64    `json:"num_for_sale"`
		LowestPrice float64  `json:"lowest_price"`
		DataQuality string   `json:"data_quality"`
		Artists     []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			ANV  string `json:"anv"`
			Join string `json:"join"`
		} `json:"artists"`
		Images []struct {
			Type        string `json:"type"`
			ResourceURL string `json:"resource_url"`
			URI         string `json:"uri"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		} `json:"images"`
		Tracklist []struct {
			Position string `json:"position"`
			Type     string `json:"type_"`
			Title    string `json:"title"`
			Duration string `json:"duration"`
		} `json:"tracklist"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Discogs master: %w", err)
	}
	title := strings.TrimSpace(source.Title)
	id := strconv.FormatInt(source.ID, 10)
	if source.ID < 1 || title == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Discogs master is missing identity or title")
	}
	record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "discogs", Namespace: "master", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.DiscogsNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "discogs", Namespace: "master", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: title, Type: "display", Primary: true}}, Classification: rgdomain.Classification{PrimaryType: "album"}, Metrics: []rgdomain.Metric{{Name: "marketplace_for_sale", Value: float64(source.NumForSale), RawValue: strconv.FormatInt(source.NumForSale, 10)}, {Name: "marketplace_lowest_price", Value: source.LowestPrice, RawValue: strconv.FormatFloat(source.LowestPrice, 'f', -1, 64)}}}
	if source.Year > 0 {
		value := strconv.Itoa(source.Year)
		record.Dates = append(record.Dates, rgdomain.DateValue{Value: value, Precision: "year", Type: "first_release"})
	}
	for i, artist := range source.Artists {
		if artist.ID < 1 {
			continue
		}
		name := strings.TrimSpace(artist.ANV)
		if name == "" {
			name = strings.TrimSpace(artist.Name)
		}
		record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Position: i, Name: name, JoinPhrase: artist.Join, ArtistProvider: "discogs", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(artist.ID, 10), ArtistName: artist.Name})
	}
	for _, name := range source.Genres {
		if name = strings.TrimSpace(name); name != "" {
			record.Genres = append(record.Genres, rgdomain.WeightedTerm{Name: name})
		}
	}
	for _, name := range source.Styles {
		if name = strings.TrimSpace(name); name != "" {
			record.Tags = append(record.Tags, rgdomain.WeightedTerm{Name: name})
		}
	}
	if source.URI != "" {
		record.Links = append(record.Links, rgdomain.Link{Type: "discogs", URL: source.URI})
	}
	for i, image := range source.Images {
		sourceURL := strings.TrimSpace(image.ResourceURL)
		if sourceURL == "" {
			sourceURL = strings.TrimSpace(image.URI)
		}
		if sourceURL != "" {
			record.Images = append(record.Images, rgdomain.Image{ProviderImageID: strconv.Itoa(i), SourceURL: sourceURL, Class: strings.ToLower(image.Type), Width: image.Width, Height: image.Height})
		}
	}
	if source.MainRelease > 0 {
		record.Editions = append(record.Editions, rgdomain.Edition{Provider: "discogs", Namespace: "release", ProviderID: strconv.FormatInt(source.MainRelease, 10), Title: title, Date: rgdomain.DateValue{Type: "release"}})
	}
	trackNumber := 0
	for _, track := range source.Tracklist {
		if track.Type != "track" || strings.TrimSpace(track.Title) == "" {
			continue
		}
		trackNumber++
		record.Tracks = append(record.Tracks, rgdomain.Track{Position: track.Position, Number: trackNumber, Title: strings.TrimSpace(track.Title), DurationMS: parseDiscogsDuration(track.Duration)})
	}
	if quality := strings.TrimSpace(source.DataQuality); quality != "" {
		record.Annotations = append(record.Annotations, rgdomain.Text{Value: quality, Type: "data_quality"})
	}
	return record, nil
}
func parseDiscogsDuration(value string) int64 {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0
	}
	minutes, err1 := strconv.ParseInt(parts[0], 10, 64)
	seconds, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || minutes < 0 || seconds < 0 || seconds > 59 {
		return 0
	}
	return (minutes*60 + seconds) * 1000
}
