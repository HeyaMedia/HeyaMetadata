package audiodb

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

type artistSource struct {
	Artist          string `json:"strArtist"`
	MusicBrainzID   string `json:"strMusicBrainzID"`
	Biography       string `json:"strBiography"`
	BiographyDE     string `json:"strBiographyDE"`
	BiographyFR     string `json:"strBiographyFR"`
	BiographyES     string `json:"strBiographyES"`
	BiographyIT     string `json:"strBiographyIT"`
	BiographyPT     string `json:"strBiographyPT"`
	BiographyNL     string `json:"strBiographyNL"`
	BiographySE     string `json:"strBiographySE"`
	BiographyNO     string `json:"strBiographyNO"`
	BiographyPL     string `json:"strBiographyPL"`
	BiographyRU     string `json:"strBiographyRU"`
	BiographyJP     string `json:"strBiographyJP"`
	Genre           string `json:"strGenre"`
	Style           string `json:"strStyle"`
	Mood            string `json:"strMood"`
	Gender          string `json:"strGender"`
	Country         string `json:"strCountry"`
	CountryCode     string `json:"strCountryCode"`
	FormedYear      string `json:"intFormedYear"`
	DiedYear        string `json:"intDiedYear"`
	Disbanded       string `json:"strDisbanded"`
	Website         string `json:"strWebsite"`
	Facebook        string `json:"strFacebook"`
	Twitter         string `json:"strTwitter"`
	Instagram       string `json:"strInstagram"`
	Popularity      string `json:"intPopularity"`
	Followers       string `json:"intFollowers"`
	ArtistThumb     string `json:"strArtistThumb"`
	ArtistLogo      string `json:"strArtistLogo"`
	ArtistBanner    string `json:"strArtistBanner"`
	ArtistFanart    string `json:"strArtistFanart"`
	ArtistFanart2   string `json:"strArtistFanart2"`
	ArtistFanart3   string `json:"strArtistFanart3"`
	ArtistFanart4   string `json:"strArtistFanart4"`
	ArtistWideThumb string `json:"strArtistWideThumb"`
	ArtistClearart  string `json:"strArtistClearart"`
	ArtistCutout    string `json:"strArtistCutout"`
}

func NormalizeArtist(body []byte, expectedMBID, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var envelope struct {
		Artists []artistSource `json:"artists"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode TheAudioDB artist: %w", err)
	}
	if len(envelope.Artists) == 0 {
		return artistdomain.NormalizedRecordV1{}, ErrNotFound
	}
	source := envelope.Artists[0]
	expectedMBID = strings.ToLower(strings.TrimSpace(expectedMBID))
	mbid := strings.ToLower(strings.TrimSpace(source.MusicBrainzID))
	name := strings.TrimSpace(source.Artist)
	if !mbidPattern.MatchString(expectedMBID) || mbid != expectedMBID || name == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("TheAudioDB artist does not match the expected MusicBrainz identity")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "audiodb", Namespace: "artist", Value: mbid, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.AudioDBNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: mbid, Confidence: 1, Evidence: "audiodb_mbid"}},
		Names:              []artistdomain.Name{{Value: name, Type: "provider"}},
		Classification:     artistdomain.Classification{Gender: strings.ToLower(strings.TrimSpace(source.Gender))},
	}
	for _, biography := range []struct{ language, value string }{
		{"en", source.Biography}, {"de", source.BiographyDE}, {"fr", source.BiographyFR},
		{"es", source.BiographyES}, {"it", source.BiographyIT}, {"pt", source.BiographyPT},
		{"nl", source.BiographyNL}, {"sv", source.BiographySE}, {"no", source.BiographyNO},
		{"pl", source.BiographyPL}, {"ru", source.BiographyRU}, {"ja", source.BiographyJP},
	} {
		if value := strings.TrimSpace(biography.value); value != "" {
			record.Biographies = append(record.Biographies, artistdomain.Text{Value: value, Language: biography.language, Type: "provider_biography", Markup: "plain"})
		}
	}
	if genre := strings.TrimSpace(source.Genre); genre != "" {
		record.Genres = append(record.Genres, artistdomain.WeightedTerm{Name: genre})
	}
	for _, tag := range []string{source.Style, source.Mood} {
		if value := strings.TrimSpace(tag); value != "" {
			record.Tags = append(record.Tags, artistdomain.WeightedTerm{Name: value})
		}
	}
	if code := strings.ToUpper(strings.TrimSpace(source.CountryCode)); len(code) == 2 {
		record.Areas = append(record.Areas, artistdomain.Area{Name: strings.TrimSpace(source.Country), Role: "country", ISOCodes: []string{code}})
	}
	if year := strings.TrimSpace(source.FormedYear); year != "" {
		if parsed, err := strconv.Atoi(year); err == nil && parsed > 1000 {
			record.Lifecycle.Dates = append(record.Lifecycle.Dates, artistdomain.DateValue{Value: year, Precision: "year", Type: "begin"})
		}
	}
	ended := strings.TrimSpace(source.DiedYear)
	if ended == "" {
		ended = strings.TrimSpace(source.Disbanded)
	}
	if parsed, err := strconv.Atoi(ended); err == nil && parsed > 1000 {
		record.Lifecycle.Dates = append(record.Lifecycle.Dates, artistdomain.DateValue{Value: ended, Precision: "year", Type: "end"})
		endedFlag := true
		record.Lifecycle.Ended = &endedFlag
	}
	for _, link := range []struct{ kind, value string }{
		{"official", source.Website}, {"facebook", source.Facebook}, {"twitter", source.Twitter}, {"instagram", source.Instagram},
	} {
		if linkURL := normalizeLinkURL(link.value); linkURL != "" {
			record.Links = append(record.Links, artistdomain.Link{Type: link.kind, URL: linkURL})
		}
	}
	for _, metric := range []struct{ name, raw string }{{"popularity", source.Popularity}, {"followers", source.Followers}} {
		if value, err := strconv.ParseFloat(strings.TrimSpace(metric.raw), 64); err == nil {
			record.Metrics = append(record.Metrics, artistdomain.Metric{Name: metric.name, Value: value, RawValue: strings.TrimSpace(metric.raw)})
		}
	}
	for _, image := range []struct{ class, id, value string }{
		{"profile", "artistthumb", source.ArtistThumb},
		{"logo", "artistlogo", source.ArtistLogo},
		{"banner", "artistbanner", source.ArtistBanner},
		{"backdrop", "artistfanart", source.ArtistFanart},
		{"backdrop", "artistfanart2", source.ArtistFanart2},
		{"backdrop", "artistfanart3", source.ArtistFanart3},
		{"backdrop", "artistfanart4", source.ArtistFanart4},
		{"backdrop", "artistwidethumb", source.ArtistWideThumb},
		{"clearart", "artistclearart", source.ArtistClearart},
		{"cutout", "artistcutout", source.ArtistCutout},
	} {
		if imageURL := normalizeLinkURL(image.value); imageURL != "" {
			record.Images = append(record.Images, artistdomain.Image{ProviderImageID: image.id, SourceURL: imageURL, Class: image.class})
		}
	}
	return record, nil
}

// normalizeLinkURL tolerates TheAudioDB's scheme-less values ("www.site.com")
// and rejects junk fields (the API returns placeholders like "1").
func normalizeLinkURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || !strings.Contains(parsed.Host, ".") {
		return ""
	}
	return parsed.String()
}
