package bandcamp

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

var (
	dataBandPattern      = regexp.MustCompile(`data-band="([^"]+)"`)
	ogImagePattern       = regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
	ogDescriptionPattern = regexp.MustCompile(`<meta property="og:description"\s+content="([^"]+)"`)
)

// NormalizeArtist parses a Bandcamp band page. Bandcamp answers unknown
// subdomains with its generic homepage (HTTP 200, a featured band's
// data-band), so identity hinges on the embedded subdomain matching the
// requested one: the homepage variant carries no subdomain and is rejected.
func NormalizeArtist(body []byte, expectedSubdomain, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	expectedSubdomain = strings.ToLower(strings.TrimSpace(expectedSubdomain))
	match := dataBandPattern.FindSubmatch(body)
	if match == nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Bandcamp page has no band metadata")
	}
	var band struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Subdomain string `json:"subdomain"`
		HTTPSURL  string `json:"https_url"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &band); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Bandcamp band metadata: %w", err)
	}
	name := strings.TrimSpace(band.Name)
	if !strings.EqualFold(strings.TrimSpace(band.Subdomain), expectedSubdomain) || name == "" || !subdomainPattern.MatchString(expectedSubdomain) {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Bandcamp page does not match the expected artist subdomain")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord: artistdomain.ProviderRecord{Provider: "bandcamp", Namespace: "artist", Value: expectedSubdomain, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.BandcampNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{
			{Provider: "bandcamp", Namespace: "artist", NormalizedValue: expectedSubdomain, Confidence: 1, Evidence: "provider_record"},
		},
		Names: []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
	}
	if band.ID > 0 {
		record.IdentityCandidates = append(record.IdentityCandidates, artistdomain.IdentityCandidate{Provider: "bandcamp", Namespace: "band", NormalizedValue: strconv.FormatInt(band.ID, 10), Confidence: 1, Evidence: "provider_record"})
	}
	link := strings.TrimSpace(band.HTTPSURL)
	if link == "" {
		link = "https://" + expectedSubdomain + ".bandcamp.com"
	}
	if parsed, err := url.Parse(link); err == nil && parsed.Scheme == "https" {
		record.Links = append(record.Links, artistdomain.Link{Type: "bandcamp", URL: link})
	}
	if match := ogImagePattern.FindSubmatch(body); match != nil {
		if image := strings.TrimSpace(html.UnescapeString(string(match[1]))); strings.HasPrefix(image, "https://") {
			record.Images = append(record.Images, artistdomain.Image{ProviderImageID: "og_image", SourceURL: image, Class: "profile"})
		}
	}
	if match := ogDescriptionPattern.FindSubmatch(body); match != nil {
		if description := strings.TrimSpace(html.UnescapeString(string(match[1]))); description != "" {
			record.Biographies = append(record.Biographies, artistdomain.Text{Value: description, Type: "provider_biography", Markup: "plain"})
		}
	}
	return record, nil
}
