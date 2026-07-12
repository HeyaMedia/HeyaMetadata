package apple

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func NormalizeAlbum(body []byte, expectedID, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	if record, ok, err := normalizeCatalogAlbum(body, expectedID, observationID, observedAt); ok || err != nil {
		return record, err
	}
	var envelope struct {
		Results []struct {
			WrapperType       string `json:"wrapperType"`
			Kind              string `json:"kind"`
			CollectionID      int64  `json:"collectionId"`
			CollectionName    string `json:"collectionName"`
			CollectionViewURL string `json:"collectionViewUrl"`
			ArtistID          int64  `json:"artistId"`
			ArtistName        string `json:"artistName"`
			ArtworkURL        string `json:"artworkUrl100"`
			TrackCount        int    `json:"trackCount"`
			Country           string `json:"country"`
			ReleaseDate       string `json:"releaseDate"`
			Genre             string `json:"primaryGenreName"`
			Explicitness      string `json:"collectionExplicitness"`
			TrackID           int64  `json:"trackId"`
			TrackName         string `json:"trackName"`
			TrackNumber       int    `json:"trackNumber"`
			DiscNumber        int    `json:"discNumber"`
			TrackTimeMS       int64  `json:"trackTimeMillis"`
			TrackExplicitness string `json:"trackExplicitness"`
			ISRC              string `json:"isrc"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Apple album: %w", err)
	}
	var album *struct {
		WrapperType       string `json:"wrapperType"`
		Kind              string `json:"kind"`
		CollectionID      int64  `json:"collectionId"`
		CollectionName    string `json:"collectionName"`
		CollectionViewURL string `json:"collectionViewUrl"`
		ArtistID          int64  `json:"artistId"`
		ArtistName        string `json:"artistName"`
		ArtworkURL        string `json:"artworkUrl100"`
		TrackCount        int    `json:"trackCount"`
		Country           string `json:"country"`
		ReleaseDate       string `json:"releaseDate"`
		Genre             string `json:"primaryGenreName"`
		Explicitness      string `json:"collectionExplicitness"`
		TrackID           int64  `json:"trackId"`
		TrackName         string `json:"trackName"`
		TrackNumber       int    `json:"trackNumber"`
		DiscNumber        int    `json:"discNumber"`
		TrackTimeMS       int64  `json:"trackTimeMillis"`
		TrackExplicitness string `json:"trackExplicitness"`
		ISRC              string `json:"isrc"`
	}
	// Re-marshal the matching collection into a named local shape so mixed lookup
	// results never let a track masquerade as the requested album.
	for _, candidate := range envelope.Results {
		if strings.EqualFold(candidate.WrapperType, "collection") && strconv.FormatInt(candidate.CollectionID, 10) == expectedID {
			encoded, _ := json.Marshal(candidate)
			album = &struct {
				WrapperType       string `json:"wrapperType"`
				Kind              string `json:"kind"`
				CollectionID      int64  `json:"collectionId"`
				CollectionName    string `json:"collectionName"`
				CollectionViewURL string `json:"collectionViewUrl"`
				ArtistID          int64  `json:"artistId"`
				ArtistName        string `json:"artistName"`
				ArtworkURL        string `json:"artworkUrl100"`
				TrackCount        int    `json:"trackCount"`
				Country           string `json:"country"`
				ReleaseDate       string `json:"releaseDate"`
				Genre             string `json:"primaryGenreName"`
				Explicitness      string `json:"collectionExplicitness"`
				TrackID           int64  `json:"trackId"`
				TrackName         string `json:"trackName"`
				TrackNumber       int    `json:"trackNumber"`
				DiscNumber        int    `json:"discNumber"`
				TrackTimeMS       int64  `json:"trackTimeMillis"`
				TrackExplicitness string `json:"trackExplicitness"`
				ISRC              string `json:"isrc"`
			}{}
			_ = json.Unmarshal(encoded, album)
			break
		}
	}
	if album == nil || strings.TrimSpace(album.CollectionName) == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Apple album %s is missing from response", expectedID)
	}
	explicit := strings.EqualFold(album.Explicitness, "explicit")
	edition := rgdomain.Edition{Provider: "apple", Namespace: "album", ProviderID: expectedID, Title: album.CollectionName, Country: album.Country, TrackCount: album.TrackCount, Explicit: &explicit, Link: album.CollectionViewURL}
	record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "apple", Namespace: "album", Value: expectedID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.AppleNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "apple", Namespace: "album", NormalizedValue: expectedID, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: album.CollectionName, Type: "edition_title", Primary: true}}, Classification: rgdomain.Classification{PrimaryType: "album"}, ArtistCredits: []rgdomain.ArtistCredit{{Position: 0, Name: album.ArtistName, ArtistProvider: "apple", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(album.ArtistID, 10), ArtistName: album.ArtistName}}, Editions: []rgdomain.Edition{edition}}
	if album.Genre != "" {
		record.Genres = append(record.Genres, rgdomain.WeightedTerm{Name: album.Genre})
	}
	if album.ReleaseDate != "" {
		date := album.ReleaseDate
		if len(date) >= 10 {
			date = date[:10]
		}
		value := rgdomain.DateValue{Value: date, Precision: "day", Type: "release"}
		record.Dates = append(record.Dates, value)
		record.Editions[0].Date = value
	}
	if album.CollectionViewURL != "" {
		record.Links = append(record.Links, rgdomain.Link{Type: "apple_music", URL: album.CollectionViewURL})
	}
	if album.ArtworkURL != "" {
		image := rgdomain.Image{SourceURL: album.ArtworkURL, Class: "cover"}
		record.Images = append(record.Images, image)
		record.Editions[0].Image = &image
	}
	for _, track := range envelope.Results {
		if !strings.EqualFold(track.WrapperType, "track") || track.CollectionID != album.CollectionID || track.TrackID < 1 {
			continue
		}
		record.Tracks = append(record.Tracks, rgdomain.Track{ProviderID: strconv.FormatInt(track.TrackID, 10), Position: strconv.Itoa(track.TrackNumber), Number: track.TrackNumber, DiscNumber: track.DiscNumber, Title: track.TrackName, DurationMS: track.TrackTimeMS, ISRC: strings.ToUpper(track.ISRC), ArtistCredits: []rgdomain.ArtistCredit{{Name: track.ArtistName, ArtistProvider: "apple", ArtistNamespace: "artist", ArtistID: strconv.FormatInt(track.ArtistID, 10), ArtistName: track.ArtistName}}})
	}
	return record, nil
}

func normalizeCatalogAlbum(body []byte, expectedID, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, bool, error) {
	type resource struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Name          string   `json:"name"`
			ArtistName    string   `json:"artistName"`
			URL           string   `json:"url"`
			UPC           string   `json:"upc"`
			ReleaseDate   string   `json:"releaseDate"`
			TrackCount    int      `json:"trackCount"`
			GenreNames    []string `json:"genreNames"`
			ContentRating string   `json:"contentRating"`
			DurationMS    int64    `json:"durationInMillis"`
			TrackNumber   int      `json:"trackNumber"`
			DiscNumber    int      `json:"discNumber"`
			ISRC          string   `json:"isrc"`
			Artwork       struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"artwork"`
		} `json:"attributes"`
		Relationships struct {
			Artists struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			} `json:"artists"`
			Tracks struct {
				Data []resource `json:"data"`
			} `json:"tracks"`
		} `json:"relationships"`
	}
	var envelope struct {
		Data []resource `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return rgdomain.NormalizedRecordV1{}, false, nil
	}
	for _, album := range envelope.Data {
		if album.Type != "albums" || album.ID != expectedID {
			continue
		}
		if strings.TrimSpace(album.Attributes.Name) == "" {
			return rgdomain.NormalizedRecordV1{}, true, fmt.Errorf("Apple Music album %s has no name", expectedID)
		}
		explicit := strings.EqualFold(album.Attributes.ContentRating, "explicit")
		edition := rgdomain.Edition{Provider: "apple", Namespace: "album", ProviderID: album.ID, Title: album.Attributes.Name, Barcode: album.Attributes.UPC, TrackCount: album.Attributes.TrackCount, Explicit: &explicit, Link: album.Attributes.URL}
		record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "apple", Namespace: "album", Value: album.ID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.AppleNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "apple", Namespace: "album", NormalizedValue: album.ID, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: album.Attributes.Name, Type: "edition_title", Primary: true}}, Classification: rgdomain.Classification{PrimaryType: "album"}, Editions: []rgdomain.Edition{edition}}
		artistID := ""
		if len(album.Relationships.Artists.Data) > 0 {
			artistID = album.Relationships.Artists.Data[0].ID
		}
		if artistID != "" || album.Attributes.ArtistName != "" {
			record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Name: album.Attributes.ArtistName, ArtistProvider: "apple", ArtistNamespace: "artist", ArtistID: artistID, ArtistName: album.Attributes.ArtistName})
		}
		for _, genre := range album.Attributes.GenreNames {
			if genre != "" {
				record.Genres = append(record.Genres, rgdomain.WeightedTerm{Name: genre})
			}
		}
		if album.Attributes.ReleaseDate != "" {
			date := rgdomain.DateValue{Value: album.Attributes.ReleaseDate, Precision: appleDatePrecision(album.Attributes.ReleaseDate), Type: "release"}
			record.Dates = append(record.Dates, date)
			record.Editions[0].Date = date
		}
		if album.Attributes.URL != "" {
			record.Links = append(record.Links, rgdomain.Link{Type: "apple_music", URL: album.Attributes.URL})
		}
		if album.Attributes.Artwork.URL != "" {
			sourceURL := strings.NewReplacer("{w}", "1200", "{h}", "1200").Replace(album.Attributes.Artwork.URL)
			image := rgdomain.Image{SourceURL: sourceURL, Class: "cover", Width: album.Attributes.Artwork.Width, Height: album.Attributes.Artwork.Height}
			record.Images = append(record.Images, image)
			record.Editions[0].Image = &image
		}
		for _, track := range album.Relationships.Tracks.Data {
			if track.Type != "songs" {
				continue
			}
			record.Tracks = append(record.Tracks, rgdomain.Track{ProviderID: track.ID, Position: strconv.Itoa(track.Attributes.TrackNumber), Number: track.Attributes.TrackNumber, DiscNumber: track.Attributes.DiscNumber, Title: track.Attributes.Name, DurationMS: track.Attributes.DurationMS, ISRC: strings.ToUpper(track.Attributes.ISRC)})
		}
		return record, true, nil
	}
	return rgdomain.NormalizedRecordV1{}, false, nil
}

func appleDatePrecision(value string) string {
	switch len(strings.Split(value, "-")) {
	case 1:
		return "year"
	case 2:
		return "month"
	default:
		return "day"
	}
}
