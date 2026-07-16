package musicbrainz

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

var allMusicAlbumPattern = regexp.MustCompile(`^mw[0-9]+$`)

type releaseGroupResponse struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Disambiguation   string         `json:"disambiguation"`
	PrimaryType      string         `json:"primary-type"`
	SecondaryTypes   []string       `json:"secondary-types"`
	FirstReleaseDate string         `json:"first-release-date"`
	Annotation       string         `json:"annotation"`
	Aliases          []alias        `json:"aliases"`
	Genres           []weightedName `json:"genres"`
	Tags             []weightedName `json:"tags"`
	Rating           struct {
		Votes int64    `json:"votes-count"`
		Value *float64 `json:"value"`
	} `json:"rating"`
	ArtistCredit []struct {
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
		Artist     struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"artist-credit"`
	Releases []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Status     string `json:"status"`
		Date       string `json:"date"`
		Country    string `json:"country"`
		TrackCount int    `json:"track-count"`
		Barcode    string `json:"barcode"`
	} `json:"releases"`
	Relations []artistRelation `json:"relations"`
}

func NormalizeReleaseGroup(body []byte, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var source releaseGroupResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode MusicBrainz release group: %w", err)
	}
	id := strings.ToLower(source.ID)
	title := strings.TrimSpace(source.Title)
	if !mbidPattern.MatchString(id) || title == "" {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("MusicBrainz release group is missing MBID or title")
	}
	record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "musicbrainz", Namespace: "release_group", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.MusicBrainzNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "release_group", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}}, Titles: []rgdomain.Title{{Value: title, Type: "display", Primary: true}}, Disambiguation: strings.TrimSpace(source.Disambiguation), Classification: rgdomain.Classification{PrimaryType: normalizeToken(source.PrimaryType), SecondaryTypes: normalizeTokens(source.SecondaryTypes)}}
	for _, alias := range source.Aliases {
		if name := strings.TrimSpace(alias.Name); name != "" {
			record.Titles = append(record.Titles, rgdomain.Title{Value: name, SortValue: strings.TrimSpace(alias.SortName), Language: normalizeLocale(alias.Locale), Type: "alias", Primary: alias.Primary != nil && *alias.Primary})
		}
	}
	if source.FirstReleaseDate != "" {
		record.Dates = append(record.Dates, rgdomain.DateValue{Value: source.FirstReleaseDate, Precision: datePrecision(source.FirstReleaseDate), Type: "first_release"})
	}
	if annotation := strings.TrimSpace(source.Annotation); annotation != "" {
		record.Annotations = append(record.Annotations, rgdomain.Text{Value: annotation, Type: "provider_annotation", Markup: "musicbrainz"})
	}
	for i, credit := range source.ArtistCredit {
		artistID := strings.ToLower(credit.Artist.ID)
		if !mbidPattern.MatchString(artistID) {
			continue
		}
		record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Position: i, Name: credit.Name, JoinPhrase: credit.JoinPhrase, ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: artistID, ArtistName: credit.Artist.Name})
	}
	for _, genre := range source.Genres {
		if name := strings.TrimSpace(genre.Name); name != "" {
			record.Genres = append(record.Genres, rgdomain.WeightedTerm{ProviderID: genre.ID, Name: name, Weight: float64(genre.Count)})
		}
	}
	for _, tag := range source.Tags {
		if name := strings.TrimSpace(tag.Name); name != "" {
			record.Tags = append(record.Tags, rgdomain.WeightedTerm{Name: name, Weight: float64(tag.Count)})
		}
	}
	if source.Rating.Value != nil {
		record.Ratings = append(record.Ratings, rgdomain.Rating{System: "musicbrainz", Value: *source.Rating.Value, ScaleMin: 0, ScaleMax: 5, Votes: source.Rating.Votes, RawValue: strconv.FormatFloat(*source.Rating.Value, 'f', -1, 64)})
	}
	for _, release := range source.Releases {
		releaseID := strings.ToLower(release.ID)
		if !mbidPattern.MatchString(releaseID) {
			continue
		}
		edition := rgdomain.Edition{Provider: "musicbrainz", Namespace: "release", ProviderID: releaseID, Title: release.Title, Status: normalizeToken(release.Status), Country: release.Country, Barcode: release.Barcode, TrackCount: release.TrackCount}
		if release.Date != "" {
			edition.Date = rgdomain.DateValue{Value: release.Date, Precision: datePrecision(release.Date), Type: "release"}
		}
		record.Editions = append(record.Editions, edition)
	}
	for _, relation := range source.Relations {
		if relation.TargetType != "url" || relation.URL == nil {
			continue
		}
		resource := strings.TrimSpace(relation.URL.Resource)
		if resource == "" {
			continue
		}
		record.Links = append(record.Links, rgdomain.Link{Type: normalizeToken(relation.Type), URL: resource})
		if candidate, ok := releaseGroupIdentityFromURL(resource); ok {
			record.IdentityCandidates = append(record.IdentityCandidates, candidate)
		}
	}
	return record, nil
}

func releaseGroupIdentityFromURL(raw string) (rgdomain.IdentityCandidate, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return rgdomain.IdentityCandidate{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	parts := pathParts(parsed.Path)
	candidate := rgdomain.IdentityCandidate{Confidence: 1, Evidence: "musicbrainz_url_relationship"}
	switch {
	case host == "discogs.com" && len(parts) >= 2 && parts[0] == "master" && numericSuffix.MatchString(parts[1]):
		candidate.Provider, candidate.Namespace, candidate.NormalizedValue = "discogs", "master", parts[1]
	case host == "wikidata.org" && len(parts) >= 2 && parts[0] == "wiki" && wikidataItemPattern.MatchString(strings.ToUpper(parts[1])):
		candidate.Provider, candidate.Namespace, candidate.NormalizedValue = "wikidata", "entity", strings.ToUpper(parts[1])
	case host == "allmusic.com" && len(parts) >= 2 && parts[0] == "album":
		value := parts[len(parts)-1]
		if at := strings.LastIndex(value, "-"); at >= 0 {
			value = value[at+1:]
		}
		if allMusicAlbumPattern.MatchString(value) {
			candidate.Provider, candidate.Namespace, candidate.NormalizedValue = "allmusic", "album", value
		}
	case strings.HasSuffix(host, ".bandcamp.com") && len(parts) >= 2 && parts[0] == "album":
		if subdomain := strings.TrimSuffix(host, ".bandcamp.com"); bandcampSubdomainPattern.MatchString(subdomain) && !bandcampSharedSubdomains[subdomain] {
			candidate.Provider, candidate.Namespace, candidate.NormalizedValue = "bandcamp", "album", subdomain+"/"+parts[1]
		}
	}
	return candidate, candidate.Provider != ""
}
func normalizeTokens(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeToken(value); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}
