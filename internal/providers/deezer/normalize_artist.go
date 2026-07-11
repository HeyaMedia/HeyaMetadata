package deezer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

func NormalizeArtist(body []byte, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var source struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Link       string `json:"link"`
		PictureXL  string `json:"picture_xl"`
		Picture    string `json:"picture"`
		AlbumCount int64  `json:"nb_album"`
		FanCount   int64  `json:"nb_fan"`
		Type       string `json:"type"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Deezer artist: %w", err)
	}
	if source.Error != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Deezer artist: %s", source.Error.Message)
	}
	name, id := strings.TrimSpace(source.Name), strconv.FormatInt(source.ID, 10)
	if source.ID < 1 || name == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Deezer artist is missing identity or name")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "deezer", Namespace: "artist", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.DeezerNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "deezer", Namespace: "artist", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}},
		Names:              []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
		Classification:     artistdomain.Classification{ArtistType: strings.ToLower(strings.TrimSpace(source.Type))},
		Metrics:            []artistdomain.Metric{{Name: "album_count", Value: float64(source.AlbumCount), RawValue: strconv.FormatInt(source.AlbumCount, 10)}, {Name: "fan_count", Value: float64(source.FanCount), RawValue: strconv.FormatInt(source.FanCount, 10)}},
	}
	if source.Link != "" {
		record.Links = append(record.Links, artistdomain.Link{Type: "deezer", URL: source.Link})
	}
	image := source.PictureXL
	if image == "" {
		image = source.Picture
	}
	if image != "" {
		record.Images = append(record.Images, artistdomain.Image{SourceURL: image, Class: "profile"})
	}
	return record, nil
}
