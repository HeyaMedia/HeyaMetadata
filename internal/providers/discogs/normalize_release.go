package discogs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func NormalizeRelease(body []byte, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var s struct {
		ID                            int64
		Title, Country, Released, URI string
		Year                          int
		DataQuality                   string `json:"data_quality"`
		Identifiers                   []struct{ Type, Value string }
		Artists                       []struct {
			ID              int64
			Name, ANV, Join string
		}
		Labels []struct {
			ID    int64
			Name  string
			CatNo string `json:"catno"`
		}
		Formats []struct {
			Name string
			Qty  string
		}
		Images []struct {
			Type        string `json:"type"`
			ResourceURL string `json:"resource_url"`
			URI         string `json:"uri"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		} `json:"images"`
		Tracklist []struct {
			Position string `json:"position"`
			Title    string `json:"title"`
			Duration string `json:"duration"`
			Type     string `json:"type_"`
		}
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Discogs release: %w", err)
	}
	if s.ID < 1 || strings.TrimSpace(s.Title) == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Discogs release missing identity or title")
	}
	id := strconv.FormatInt(s.ID, 10)
	barcode := ""
	for _, x := range s.Identifiers {
		if strings.EqualFold(x.Type, "Barcode") {
			barcode = x.Value
			break
		}
	}
	date := rgdomain.DateValue{Value: s.Released, Precision: "day", Type: "release"}
	e := rgdomain.Edition{Provider: "discogs", Namespace: "release", ProviderID: id, Title: s.Title, Country: s.Country, Barcode: barcode, Date: date, Link: s.URI}
	r := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "discogs", Namespace: "release", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: "discogs-release/v1", SchemaVersion: 1}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "discogs", Namespace: "release", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: s.Title, Type: "edition_title", Primary: true}}, Dates: []rgdomain.DateValue{date}, Editions: []rgdomain.Edition{e}}
	for i, a := range s.Artists {
		name := a.ANV
		if name == "" {
			name = a.Name
		}
		r.ArtistCredits = append(r.ArtistCredits, rgdomain.ArtistCredit{Position: i, Name: name, JoinPhrase: a.Join, ArtistProvider: "discogs", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(a.ID, 10), ArtistName: a.Name})
	}
	number := 0
	for _, t := range s.Tracklist {
		if t.Type != "track" || strings.TrimSpace(t.Title) == "" {
			continue
		}
		number++
		r.Tracks = append(r.Tracks, rgdomain.Track{Position: t.Position, Number: number, Title: t.Title, DurationMS: parseDiscogsDuration(t.Duration)})
	}
	r.Editions[0].TrackCount = len(r.Tracks)
	for i, image := range s.Images {
		sourceURL := strings.TrimSpace(image.ResourceURL)
		if sourceURL == "" {
			sourceURL = strings.TrimSpace(image.URI)
		}
		if sourceURL == "" {
			continue
		}
		candidate := rgdomain.Image{ProviderImageID: strconv.Itoa(i), SourceURL: sourceURL, Class: "cover", Width: image.Width, Height: image.Height}
		r.Images = append(r.Images, candidate)
		if i == 0 {
			r.Editions[0].Image = &candidate
		}
	}
	return r, nil
}
