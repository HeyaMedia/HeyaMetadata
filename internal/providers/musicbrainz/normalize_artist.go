package musicbrainz

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

var numericSuffix = regexp.MustCompile(`^[0-9]+$`)
var wikidataItemPattern = regexp.MustCompile(`^Q[1-9][0-9]*$`)

// bandcampSubdomainPattern accepts one DNS label of letters/digits/hyphens;
// bandcampSharedSubdomains lists Bandcamp-operated hosts that are never an
// artist's own page.
var bandcampSubdomainPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var bandcampSharedSubdomains = map[string]bool{"daily": true, "blog": true, "get": true, "help": true, "shop": true}

type artistResponse struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	SortName       string           `json:"sort-name"`
	Disambiguation string           `json:"disambiguation"`
	Type           string           `json:"type"`
	Gender         string           `json:"gender"`
	Country        string           `json:"country"`
	Area           *area            `json:"area"`
	BeginArea      *area            `json:"begin-area"`
	EndArea        *area            `json:"end-area"`
	LifeSpan       lifeSpan         `json:"life-span"`
	ISNIs          []string         `json:"isnis"`
	IPIs           []string         `json:"ipis"`
	Aliases        []alias          `json:"aliases"`
	Annotation     string           `json:"annotation"`
	Genres         []weightedName   `json:"genres"`
	Tags           []weightedName   `json:"tags"`
	Relations      []artistRelation `json:"relations"`
}

type area struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	SortName string   `json:"sort-name"`
	ISO1     []string `json:"iso-3166-1-codes"`
	ISO2     []string `json:"iso-3166-2-codes"`
	ISO3     []string `json:"iso-3166-3-codes"`
}

type lifeSpan struct {
	Begin string `json:"begin"`
	End   string `json:"end"`
	Ended *bool  `json:"ended"`
}

type alias struct {
	Name     string `json:"name"`
	SortName string `json:"sort-name"`
	Locale   string `json:"locale"`
	Type     string `json:"type"`
	Primary  *bool  `json:"primary"`
	Begin    string `json:"begin"`
	End      string `json:"end"`
}

type weightedName struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type artistRelation struct {
	Type       string   `json:"type"`
	Direction  string   `json:"direction"`
	TargetType string   `json:"target-type"`
	Begin      string   `json:"begin"`
	End        string   `json:"end"`
	Ended      *bool    `json:"ended"`
	Attributes []string `json:"attributes"`
	URL        *struct {
		Resource string `json:"resource"`
	} `json:"url"`
	Artist *struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SortName       string `json:"sort-name"`
		Disambiguation string `json:"disambiguation"`
	} `json:"artist"`
}

func NormalizeArtist(body []byte, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var source artistResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode MusicBrainz artist: %w", err)
	}
	if !mbidPattern.MatchString(strings.ToLower(source.ID)) || strings.TrimSpace(source.Name) == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("MusicBrainz artist is missing MBID or name")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord: artistdomain.ProviderRecord{
			Provider: "musicbrainz", Namespace: "artist", Value: strings.ToLower(source.ID),
			PrimaryObservationID: observationID, ObservedAt: observedAt,
			NormalizerVersion: artistdomain.MusicBrainzNormalizerVersion,
			SchemaVersion:     artistdomain.NormalizedSchemaVersion,
		},
		IdentityCandidates: []artistdomain.IdentityCandidate{{
			Provider: "musicbrainz", Namespace: "artist", NormalizedValue: strings.ToLower(source.ID),
			Confidence: 1, Evidence: "provider_record",
		}},
		Names:          []artistdomain.Name{{Value: strings.TrimSpace(source.Name), SortValue: strings.TrimSpace(source.SortName), Type: "display", Primary: true}},
		Disambiguation: strings.TrimSpace(source.Disambiguation),
		Classification: artistdomain.Classification{ArtistType: normalizeToken(source.Type), Gender: normalizeToken(source.Gender)},
		Lifecycle:      artistdomain.Lifecycle{Ended: source.LifeSpan.Ended},
	}
	if source.SortName != "" && source.SortName != source.Name {
		record.Names = append(record.Names, artistdomain.Name{Value: source.SortName, Type: "sort"})
	}
	for _, value := range source.Aliases {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		aliasType := normalizeToken(value.Type)
		if aliasType == "" {
			aliasType = "alias"
		}
		record.Names = append(record.Names, artistdomain.Name{
			Value: name, SortValue: strings.TrimSpace(value.SortName), Language: normalizeLocale(value.Locale),
			Type: aliasType, Primary: value.Primary != nil && *value.Primary,
			BeginDate: value.Begin, EndDate: value.End,
		})
	}
	appendArea := func(role string, value *area) {
		if value == nil || strings.TrimSpace(value.Name) == "" {
			return
		}
		codes := append(append(append([]string{}, value.ISO1...), value.ISO2...), value.ISO3...)
		record.Areas = append(record.Areas, artistdomain.Area{ProviderID: value.ID, Name: value.Name, SortName: value.SortName, Role: role, ISOCodes: codes})
	}
	appendArea("primary", source.Area)
	appendArea("begin", source.BeginArea)
	appendArea("end", source.EndArea)
	if source.Country != "" && !areaHasCode(record.Areas, source.Country) {
		record.Areas = append(record.Areas, artistdomain.Area{Name: source.Country, Role: "country", ISOCodes: []string{source.Country}})
	}
	appendDate := func(kind, value string) {
		if value != "" {
			record.Lifecycle.Dates = append(record.Lifecycle.Dates, artistdomain.DateValue{Value: value, Precision: datePrecision(value), Type: kind})
		}
	}
	appendDate("begin", source.LifeSpan.Begin)
	appendDate("end", source.LifeSpan.End)
	for _, value := range source.ISNIs {
		if normalized := strings.ReplaceAll(strings.TrimSpace(value), " ", ""); normalized != "" {
			record.IdentityCandidates = append(record.IdentityCandidates, artistdomain.IdentityCandidate{Provider: "isni", Namespace: "artist", NormalizedValue: normalized, Confidence: 1, Evidence: "musicbrainz_authority_id"})
		}
	}
	for _, value := range source.IPIs {
		if normalized := strings.TrimSpace(value); normalized != "" {
			record.IdentityCandidates = append(record.IdentityCandidates, artistdomain.IdentityCandidate{Provider: "ipi", Namespace: "artist", NormalizedValue: normalized, Confidence: 1, Evidence: "musicbrainz_authority_id"})
		}
	}
	if annotation := strings.TrimSpace(source.Annotation); annotation != "" {
		record.Annotations = append(record.Annotations, artistdomain.Text{Value: annotation, Type: "provider_annotation", Markup: "musicbrainz"})
	}
	for _, value := range source.Genres {
		if name := strings.TrimSpace(value.Name); name != "" {
			record.Genres = append(record.Genres, artistdomain.WeightedTerm{ProviderID: value.ID, Name: name, Weight: float64(value.Count)})
		}
	}
	for _, value := range source.Tags {
		if name := strings.TrimSpace(value.Name); name != "" {
			record.Tags = append(record.Tags, artistdomain.WeightedTerm{Name: name, Weight: float64(value.Count)})
		}
	}
	for _, relation := range source.Relations {
		if relation.TargetType == "url" && relation.URL != nil {
			resource := strings.TrimSpace(relation.URL.Resource)
			if resource == "" {
				continue
			}
			record.Links = append(record.Links, artistdomain.Link{Type: normalizeToken(relation.Type), URL: resource})
			if candidate, ok := identityFromURL(resource); ok {
				record.IdentityCandidates = append(record.IdentityCandidates, candidate)
			}
			continue
		}
		if relation.TargetType == "artist" && relation.Artist != nil && mbidPattern.MatchString(strings.ToLower(relation.Artist.ID)) {
			record.Relationships = append(record.Relationships, artistdomain.Relationship{
				Type: normalizeToken(relation.Type), Direction: relation.Direction,
				TargetProvider: "musicbrainz", TargetNamespace: "artist", TargetID: strings.ToLower(relation.Artist.ID),
				TargetName: relation.Artist.Name, BeginDate: relation.Begin, EndDate: relation.End,
				Ended: relation.Ended, Attributes: relation.Attributes,
			})
		}
	}
	return record, nil
}

func identityFromURL(raw string) (artistdomain.IdentityCandidate, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return artistdomain.IdentityCandidate{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	parts := pathParts(parsed.Path)
	candidate := artistdomain.IdentityCandidate{Namespace: "artist", Confidence: 1, Evidence: "musicbrainz_url_relationship"}
	switch {
	case host == "discogs.com" && len(parts) >= 2 && parts[0] == "artist" && numericSuffix.MatchString(parts[1]):
		candidate.Provider, candidate.NormalizedValue = "discogs", parts[1]
	case host == "deezer.com" && len(parts) >= 2 && parts[len(parts)-2] == "artist" && numericSuffix.MatchString(parts[len(parts)-1]):
		candidate.Provider, candidate.NormalizedValue = "deezer", parts[len(parts)-1]
	case (host == "music.apple.com" || host == "itunes.apple.com"):
		for i := len(parts) - 1; i >= 0; i-- {
			value := strings.TrimPrefix(parts[i], "id")
			if numericSuffix.MatchString(value) {
				candidate.Provider, candidate.NormalizedValue = "apple", value
				break
			}
		}
	case host == "wikidata.org" && len(parts) >= 2 && parts[0] == "wiki" && wikidataItemPattern.MatchString(strings.ToUpper(parts[1])):
		candidate.Provider, candidate.Namespace, candidate.NormalizedValue = "wikidata", "entity", strings.ToUpper(parts[1])
	case host == "open.spotify.com" && len(parts) >= 2 && parts[0] == "artist":
		candidate.Provider, candidate.NormalizedValue = "spotify", parts[1]
	case (host == "tidal.com" || host == "listen.tidal.com") && len(parts) >= 2 && parts[len(parts)-2] == "artist" && numericSuffix.MatchString(parts[len(parts)-1]):
		candidate.Provider, candidate.NormalizedValue = "tidal", parts[len(parts)-1]
	case strings.HasSuffix(host, ".bandcamp.com"):
		if subdomain := strings.TrimSuffix(host, ".bandcamp.com"); bandcampSubdomainPattern.MatchString(subdomain) && !bandcampSharedSubdomains[subdomain] {
			candidate.Provider, candidate.NormalizedValue = "bandcamp", subdomain
		}
	}
	return candidate, candidate.Provider != "" && candidate.NormalizedValue != ""
}

func pathParts(path string) []string {
	var result []string
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func normalizeToken(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}

func normalizeLocale(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "_", "-")
}

func datePrecision(value string) string {
	switch len(strings.Split(value, "-")) {
	case 1:
		return "year"
	case 2:
		return "month"
	case 3:
		return "day"
	default:
		return "unknown"
	}
}

func areaHasCode(areas []artistdomain.Area, code string) bool {
	for _, area := range areas {
		for _, candidate := range area.ISOCodes {
			if candidate == code {
				return true
			}
		}
	}
	return false
}
