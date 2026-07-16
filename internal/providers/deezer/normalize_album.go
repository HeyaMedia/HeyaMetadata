package deezer

import (
	"encoding/json"
	"fmt"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"strconv"
	"strings"
	"time"
)

func NormalizeAlbum(body []byte, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var source struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		UPC         string `json:"upc"`
		Link        string `json:"link"`
		CoverXL     string `json:"cover_xl"`
		ReleaseDate string `json:"release_date"`
		RecordType  string `json:"record_type"`
		Label       string `json:"label"`
		Explicit    bool   `json:"explicit_lyrics"`
		Duration    int64  `json:"duration"`
		TrackCount  int    `json:"nb_tracks"`
		Fans        int64  `json:"fans"`
		Genres      struct {
			Data []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		} `json:"genres"`
		Contributors []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"contributors"`
		Tracks struct {
			Data []struct {
				ID         int64  `json:"id"`
				Title      string `json:"title"`
				TitleShort string `json:"title_short"`
				Duration   int64  `json:"duration"`
				ISRC       string `json:"isrc"`
				Preview    string `json:"preview"`
				DiskNumber int    `json:"disk_number"`
				Artist     struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"data"`
		} `json:"tracks"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Deezer album: %w", err)
	}
	if source.Error != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Deezer album: %s", source.Error.Message)
	}
	title := strings.TrimSpace(source.Title)
	id := strconv.FormatInt(source.ID, 10)
	if source.ID < 1 || title == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Deezer album is missing identity or title")
	}
	date := rgdomain.DateValue{Value: source.ReleaseDate, Precision: "day", Type: "release"}
	edition := rgdomain.Edition{Provider: "deezer", Namespace: "album", ProviderID: id, Title: title, Date: date, Barcode: source.UPC, TrackCount: source.TrackCount, DurationMS: source.Duration * 1000, Explicit: &source.Explicit, Link: source.Link}
	if label := strings.TrimSpace(source.Label); label != "" {
		edition.Labels = append(edition.Labels, rgdomain.Label{Name: label})
	}
	record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "deezer", Namespace: "album", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.DeezerNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "deezer", Namespace: "album", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: title, Type: "edition_title", Primary: true}}, Classification: rgdomain.Classification{PrimaryType: strings.ToLower(source.RecordType)}, Dates: []rgdomain.DateValue{date}, Editions: []rgdomain.Edition{edition}, Metrics: []rgdomain.Metric{{Name: "fan_count", Value: float64(source.Fans), RawValue: strconv.FormatInt(source.Fans, 10)}}}
	for i, artist := range source.Contributors {
		if artist.ID > 0 {
			record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Position: i, Name: artist.Name, Role: strings.ToLower(strings.TrimSpace(artist.Role)), ArtistProvider: "deezer", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(artist.ID, 10), ArtistName: artist.Name})
		}
	}
	for _, genre := range source.Genres.Data {
		if genre.Name != "" {
			record.Genres = append(record.Genres, rgdomain.WeightedTerm{ProviderID: strconv.FormatInt(genre.ID, 10), Name: genre.Name})
		}
	}
	if source.Link != "" {
		record.Links = append(record.Links, rgdomain.Link{Type: "deezer", URL: source.Link})
	}
	if source.CoverXL != "" {
		image := rgdomain.Image{SourceURL: source.CoverXL, Class: "cover"}
		record.Images = append(record.Images, image)
		record.Editions[0].Image = &image
	}
	for i, track := range source.Tracks.Data {
		title := track.TitleShort
		if title == "" {
			title = track.Title
		}
		discNumber := track.DiskNumber
		if discNumber < 1 {
			discNumber = 1
		}
		record.Tracks = append(record.Tracks, rgdomain.Track{ProviderID: strconv.FormatInt(track.ID, 10), Position: strconv.Itoa(i + 1), Number: i + 1, DiscNumber: discNumber, Title: title, DurationMS: track.Duration * 1000, ISRC: strings.ToUpper(track.ISRC), PreviewURL: track.Preview, ArtistCredits: []rgdomain.ArtistCredit{{Name: track.Artist.Name, ArtistProvider: "deezer", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(track.Artist.ID, 10), ArtistName: track.Artist.Name}}})
	}
	return record, nil
}
