package audiodb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

type albumSource struct {
	Album               string `json:"strAlbum"`
	MusicBrainzID       string `json:"strMusicBrainzID"`
	MusicBrainzArtistID string `json:"strMusicBrainzArtistID"`
	YearReleased        string `json:"intYearReleased"`
	Genre               string `json:"strGenre"`
	Style               string `json:"strStyle"`
	Mood                string `json:"strMood"`
	Label               string `json:"strLabel"`
	ReleaseFormat       string `json:"strReleaseFormat"`
	Sales               string `json:"intSales"`
	Score               string `json:"intScore"`
	ScoreVotes          string `json:"intScoreVotes"`
	Description         string `json:"strDescription"`
	DescriptionDE       string `json:"strDescriptionDE"`
	DescriptionFR       string `json:"strDescriptionFR"`
	DescriptionES       string `json:"strDescriptionES"`
	DescriptionIT       string `json:"strDescriptionIT"`
	DescriptionPT       string `json:"strDescriptionPT"`
	Review              string `json:"strReview"`
	AlbumThumb          string `json:"strAlbumThumb"`
	AlbumBack           string `json:"strAlbumBack"`
	AlbumCDart          string `json:"strAlbumCDart"`
	AlbumSpine          string `json:"strAlbumSpine"`
	Album3DCase         string `json:"strAlbum3DCase"`
	Album3DFlat         string `json:"strAlbum3DFlat"`
	Album3DFace         string `json:"strAlbum3DFace"`
}

func NormalizeAlbum(body []byte, expectedMBID, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var envelope struct {
		Albums []albumSource `json:"album"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode TheAudioDB album: %w", err)
	}
	if len(envelope.Albums) == 0 {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("TheAudioDB has no album for this MusicBrainz release group")
	}
	source := envelope.Albums[0]
	expectedMBID = strings.ToLower(strings.TrimSpace(expectedMBID))
	mbid := strings.ToLower(strings.TrimSpace(source.MusicBrainzID))
	title := strings.TrimSpace(source.Album)
	if !mbidPattern.MatchString(expectedMBID) || mbid != expectedMBID || title == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("TheAudioDB album does not match the expected MusicBrainz identity")
	}
	record := rgdomain.NormalizedRecordV1{
		ProviderRecord:     rgdomain.ProviderRecord{Provider: "audiodb", Namespace: "release_group", Value: mbid, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.AudioDBNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion},
		IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "release_group", NormalizedValue: mbid, Confidence: 1, Evidence: "audiodb_mbid"}},
		Titles:             []rgdomain.Title{{Value: title, Type: "provider"}},
	}
	for _, description := range []struct{ language, value string }{
		{"en", source.Description}, {"de", source.DescriptionDE}, {"fr", source.DescriptionFR},
		{"es", source.DescriptionES}, {"it", source.DescriptionIT}, {"pt", source.DescriptionPT},
	} {
		if value := strings.TrimSpace(description.value); value != "" {
			record.Descriptions = append(record.Descriptions, rgdomain.Text{Value: value, Language: description.language, Type: "provider_description", Markup: "plain"})
		}
	}
	if review := strings.TrimSpace(source.Review); review != "" {
		record.Annotations = append(record.Annotations, rgdomain.Text{Value: review, Language: "en", Type: "provider_review", Markup: "plain"})
	}
	if genre := strings.TrimSpace(source.Genre); genre != "" {
		record.Genres = append(record.Genres, rgdomain.WeightedTerm{Name: genre})
	}
	for _, tag := range []string{source.Style, source.Mood} {
		if value := strings.TrimSpace(tag); value != "" {
			record.Tags = append(record.Tags, rgdomain.WeightedTerm{Name: value})
		}
	}
	if year := strings.TrimSpace(source.YearReleased); year != "" {
		if parsed, err := strconv.Atoi(year); err == nil && parsed > 1000 {
			record.Dates = append(record.Dates, rgdomain.DateValue{Value: year, Precision: "year", Type: "release"})
		}
	}
	if score, err := strconv.ParseFloat(strings.TrimSpace(source.Score), 64); err == nil && score > 0 {
		votes, _ := strconv.ParseInt(strings.TrimSpace(source.ScoreVotes), 10, 64)
		record.Ratings = append(record.Ratings, rgdomain.Rating{System: "audiodb", Value: score, ScaleMin: 0, ScaleMax: 10, Votes: votes, RawValue: strings.TrimSpace(source.Score)})
	}
	if sales, err := strconv.ParseFloat(strings.TrimSpace(source.Sales), 64); err == nil && sales > 0 {
		record.Metrics = append(record.Metrics, rgdomain.Metric{Name: "sales", Value: sales, RawValue: strings.TrimSpace(source.Sales)})
	}
	for _, image := range []struct{ class, id, value string }{
		{"cover", "albumthumb", source.AlbumThumb},
		{"back", "albumback", source.AlbumBack},
		{"cdart", "albumcdart", source.AlbumCDart},
		{"spine", "albumspine", source.AlbumSpine},
		{"case", "album3dcase", source.Album3DCase},
		{"flat", "album3dflat", source.Album3DFlat},
		{"face", "album3dface", source.Album3DFace},
	} {
		if imageURL := normalizeLinkURL(image.value); imageURL != "" {
			record.Images = append(record.Images, rgdomain.Image{ProviderImageID: image.id, SourceURL: imageURL, Class: image.class})
		}
	}
	return record, nil
}
