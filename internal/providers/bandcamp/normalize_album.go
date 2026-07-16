package bandcamp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

var (
	jsonLDPattern = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)
	// Bandcamp writes nonstandard ISO-8601-ish durations like P00H04M48S
	// (no T separator).
	durationPattern = regexp.MustCompile(`^P(?:(\d+)D)?T?(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)
)

type jsonLDAlbum struct {
	ID            string `json:"@id"`
	Type          string `json:"@type"`
	Name          string `json:"name"`
	DatePublished string `json:"datePublished"`
	NumTracks     int    `json:"numTracks"`
	Image         string `json:"image"`
	Keywords      []string
	ByArtist      struct {
		Name string `json:"name"`
		ID   string `json:"@id"`
	} `json:"byArtist"`
	AlbumReleaseType string `json:"albumReleaseType"`
	AlbumRelease     []struct {
		MusicReleaseFormat string `json:"musicReleaseFormat"`
	} `json:"albumRelease"`
	Track struct {
		ItemListElement []struct {
			Position int `json:"position"`
			Item     struct {
				ID       string `json:"@id"`
				Name     string `json:"name"`
				Duration string `json:"duration"`
			} `json:"item"`
		} `json:"itemListElement"`
	} `json:"track"`
}

// UnmarshalJSON tolerates keywords being either a string or a list.
func (a *jsonLDAlbum) UnmarshalJSON(body []byte) error {
	type albumAlias jsonLDAlbum
	var alias struct {
		albumAlias
		Keywords json.RawMessage `json:"keywords"`
	}
	if err := json.Unmarshal(body, &alias); err != nil {
		return err
	}
	*a = jsonLDAlbum(alias.albumAlias)
	var list []string
	if json.Unmarshal(alias.Keywords, &list) == nil {
		a.Keywords = list
	} else {
		var single string
		if json.Unmarshal(alias.Keywords, &single) == nil && single != "" {
			a.Keywords = []string{single}
		}
	}
	return nil
}

// NormalizeAlbum parses the schema.org MusicAlbum JSON-LD Bandcamp embeds in
// album pages. Identity hinges on the JSON-LD @id matching the requested
// subdomain/slug pair; unknown slugs land on pages without a MusicAlbum block.
func NormalizeAlbum(body []byte, expectedValue, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	expectedValue = strings.ToLower(strings.TrimSpace(expectedValue))
	parts := strings.SplitN(expectedValue, "/", 2)
	if len(parts) != 2 {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Bandcamp album identity requires a subdomain/slug pair")
	}
	match := jsonLDPattern.FindSubmatch(body)
	if match == nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Bandcamp album page has no JSON-LD metadata")
	}
	var album jsonLDAlbum
	if err := json.Unmarshal(match[1], &album); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Bandcamp album JSON-LD: %w", err)
	}
	title := strings.TrimSpace(album.Name)
	if album.Type != "MusicAlbum" || title == "" || !albumIDMatches(album.ID, parts[0], parts[1]) {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Bandcamp album page does not match the expected album identity")
	}
	record := rgdomain.NormalizedRecordV1{
		ProviderRecord:     rgdomain.ProviderRecord{Provider: "bandcamp", Namespace: "album", Value: expectedValue, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.BandcampNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion},
		IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "bandcamp", Namespace: "album", NormalizedValue: expectedValue, Confidence: 1, Evidence: "provider_record"}},
		Titles:             []rgdomain.Title{{Value: title, Type: "edition_title", Primary: true}},
		Classification:     rgdomain.Classification{PrimaryType: normalizeReleaseType(album.AlbumReleaseType)},
	}
	edition := rgdomain.Edition{Provider: "bandcamp", Namespace: "album", ProviderID: expectedValue, Title: title, TrackCount: album.NumTracks, Link: strings.TrimSpace(album.ID)}
	if date := parseBandcampDate(album.DatePublished); date != "" {
		value := rgdomain.DateValue{Value: date, Precision: "day", Type: "release"}
		record.Dates = append(record.Dates, value)
		edition.Date = value
	}
	for _, release := range album.AlbumRelease {
		if format := normalizeReleaseFormat(release.MusicReleaseFormat); format != "" {
			edition.Formats = append(edition.Formats, format)
		}
	}
	if name := strings.TrimSpace(album.ByArtist.Name); name != "" {
		record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Name: name, ArtistProvider: "bandcamp", ArtistNamespace: "artist", ArtistID: parts[0], ArtistName: name})
	}
	for _, keyword := range album.Keywords {
		if value := strings.TrimSpace(keyword); value != "" {
			record.Tags = append(record.Tags, rgdomain.WeightedTerm{Name: value})
		}
	}
	if image := strings.TrimSpace(album.Image); strings.HasPrefix(image, "https://") {
		candidate := rgdomain.Image{ProviderImageID: "album_art", SourceURL: image, Class: "cover"}
		record.Images = append(record.Images, candidate)
		edition.Image = &candidate
	}
	for index, item := range album.Track.ItemListElement {
		name := strings.TrimSpace(item.Item.Name)
		if name == "" {
			continue
		}
		position := item.Position
		if position < 1 {
			position = index + 1
		}
		record.Tracks = append(record.Tracks, rgdomain.Track{Position: strconv.Itoa(position), Number: position, Title: name, DurationMS: parseBandcampDuration(item.Item.Duration)})
	}
	if edition.TrackCount == 0 {
		edition.TrackCount = len(record.Tracks)
	}
	record.Editions = []rgdomain.Edition{edition}
	return record, nil
}

func albumIDMatches(rawID, subdomain, slug string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return host == subdomain+".bandcamp.com" && len(segments) == 2 && segments[0] == "album" && strings.EqualFold(segments[1], slug)
}

// parseBandcampDate converts "09 Aug 2024 00:00:00 GMT" to "2024-08-09".
func parseBandcampDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := time.Parse("02 Jan 2006 15:04:05 MST", raw)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

func parseBandcampDuration(raw string) int64 {
	match := durationPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return 0
	}
	units := []int64{24 * 3600, 3600, 60, 1}
	var seconds int64
	for index, value := range match[1:] {
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0
		}
		seconds += parsed * units[index]
	}
	return seconds * 1000
}

func normalizeReleaseType(value string) string {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "Release"))
	switch value {
	case "album", "single", "ep":
		return value
	}
	return ""
}

func normalizeReleaseFormat(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), "Format")
	if value == "" {
		return ""
	}
	return value
}
