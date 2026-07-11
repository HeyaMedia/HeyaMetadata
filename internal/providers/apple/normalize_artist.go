package apple

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

type lookupEnvelope struct {
	Results []struct {
		WrapperType      string `json:"wrapperType"`
		ArtistType       string `json:"artistType"`
		ArtistName       string `json:"artistName"`
		ArtistLinkURL    string `json:"artistLinkUrl"`
		ArtistID         int64  `json:"artistId"`
		PrimaryGenreName string `json:"primaryGenreName"`
		PrimaryGenreID   int64  `json:"primaryGenreId"`
	} `json:"results"`
}

type catalogEnvelope struct {
	Data []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Name       string   `json:"name"`
			GenreNames []string `json:"genreNames"`
			URL        string   `json:"url"`
		} `json:"attributes"`
	} `json:"data"`
}

// NormalizeArtist accepts both the public iTunes lookup response and the
// authenticated Apple Music catalog response used by Client.Collect.
func NormalizeArtist(body []byte, expectedID, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	expectedID = strings.TrimSpace(expectedID)
	var lookup lookupEnvelope
	if err := json.Unmarshal(body, &lookup); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Apple artist: %w", err)
	}
	for _, source := range lookup.Results {
		id := strconv.FormatInt(source.ArtistID, 10)
		if strings.EqualFold(source.WrapperType, "artist") && id == expectedID {
			return appleRecord(id, source.ArtistName, source.ArtistType, source.ArtistLinkURL,
				[]artistdomain.WeightedTerm{{ProviderID: strconv.FormatInt(source.PrimaryGenreID, 10), Name: source.PrimaryGenreName}},
				observationID, observedAt)
		}
	}
	var catalog catalogEnvelope
	if err := json.Unmarshal(body, &catalog); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Apple Music artist: %w", err)
	}
	for _, source := range catalog.Data {
		if source.Type != "artists" || source.ID != expectedID {
			continue
		}
		genres := make([]artistdomain.WeightedTerm, 0, len(source.Attributes.GenreNames))
		for _, name := range source.Attributes.GenreNames {
			genres = append(genres, artistdomain.WeightedTerm{Name: name})
		}
		return appleRecord(source.ID, source.Attributes.Name, "artist", source.Attributes.URL, genres, observationID, observedAt)
	}
	return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Apple artist %s is missing from response", expectedID)
}

func appleRecord(id, name, artistType, link string, genres []artistdomain.WeightedTerm, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Apple artist is missing identity or name")
	}
	cleanGenres := genres[:0]
	for _, genre := range genres {
		if genre.Name = strings.TrimSpace(genre.Name); genre.Name != "" {
			cleanGenres = append(cleanGenres, genre)
		}
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "apple", Namespace: "artist", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.AppleNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "apple", Namespace: "artist", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}},
		Names:              []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
		Classification:     artistdomain.Classification{ArtistType: strings.ToLower(strings.TrimSpace(artistType))},
		Genres:             cleanGenres,
	}
	if strings.TrimSpace(link) != "" {
		record.Links = append(record.Links, artistdomain.Link{Type: "apple_music", URL: link})
	}
	return record, nil
}
