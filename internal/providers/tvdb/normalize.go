package tvdb

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
)

func Normalize(detailBody []byte, observationID string, supportingIDs []string, observedAt time.Time) (moviedomain.NormalizedRecordV1, error) {
	var wrapper envelope[movie]
	if err := json.Unmarshal(detailBody, &wrapper); err != nil {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("decode TVDB movie: %w", err)
	}
	source := wrapper.Data
	if source.ID < 1 || strings.TrimSpace(source.Name) == "" {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("TVDB movie is missing identity or title")
	}
	record := moviedomain.NormalizedRecordV1{
		ProviderRecord: moviedomain.ProviderRecord{
			Provider: "tvdb", Namespace: "movie", Value: strconv.FormatInt(source.ID, 10),
			PrimaryObservationID: observationID, SupportingObservationIDs: supportingIDs,
			ObservedAt: observedAt, NormalizerVersion: moviedomain.TVDBNormalizerVersion,
			SchemaVersion: moviedomain.NormalizedSchemaVersion,
		},
		IdentityCandidates: []moviedomain.IdentityCandidate{{
			Provider: "tvdb", Namespace: "movie", NormalizedValue: strconv.FormatInt(source.ID, 10),
			Confidence: 1, Evidence: "provider_record",
		}},
		Titles: []moviedomain.LocalizedText{{Value: strings.TrimSpace(source.Name), Type: "display"}},
		Classification: moviedomain.Classification{
			ProviderMediaType: "movie", OriginalLanguage: source.OriginalLanguage,
			SpokenLanguages: append([]string(nil), source.SpokenLanguages...),
		},
		Lifecycle: moviedomain.Lifecycle{RawStatus: source.Status.Name, NormalizedStatus: normalizeStatus(source.Status.Name)},
	}
	for _, country := range source.ProductionCountries {
		if value := strings.ToUpper(strings.TrimSpace(country.Country)); value != "" {
			record.Classification.Countries = appendUnique(record.Classification.Countries, value)
		}
	}
	if value := strings.ToUpper(strings.TrimSpace(source.OriginalCountry)); value != "" {
		record.Classification.Countries = appendUnique(record.Classification.Countries, value)
	}
	for _, genre := range source.Genres {
		if value := strings.TrimSpace(genre.Name); value != "" {
			record.Classification.Genres = appendUnique(record.Classification.Genres, value)
			if strings.EqualFold(value, "animation") || strings.EqualFold(value, "anime") {
				record.Classification.AnimationEvidence = true
			}
		}
	}
	for _, tag := range source.TagOptions {
		if value := strings.TrimSpace(tag.Name); value != "" {
			record.Classification.Keywords = appendUnique(record.Classification.Keywords, value)
		}
	}
	for _, alias := range source.Aliases {
		if value := strings.TrimSpace(alias.Name); value != "" {
			record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: value, Language: alias.Language, Type: "alias"})
		}
	}
	for _, translation := range source.Translations.Name {
		if value := strings.TrimSpace(translation.Name); value != "" {
			titleType := "translated"
			if translation.IsPrimary {
				titleType = "display"
			}
			record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: value, Language: translation.Language, Type: titleType})
		}
		for _, alias := range translation.Aliases {
			if value := strings.TrimSpace(alias); value != "" {
				record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: value, Language: translation.Language, Type: "alias"})
			}
		}
	}
	for _, translation := range source.Translations.Overview {
		if value := strings.TrimSpace(translation.Overview); value != "" {
			record.Descriptions = append(record.Descriptions, moviedomain.LocalizedText{Value: value, Language: translation.Language, Type: "overview"})
		}
	}
	for _, remote := range source.RemoteIDs {
		if candidate, ok := remoteCandidate(remote); ok {
			record.IdentityCandidates = append(record.IdentityCandidates, candidate)
		}
	}
	for _, release := range source.Releases {
		if date := normalizeDate(release.Date); date != "" {
			record.Lifecycle.ReleaseEvents = append(record.Lifecycle.ReleaseEvents, moviedomain.ReleaseEvent{
				Country: strings.ToUpper(release.Country), Type: "release", Date: date,
				Certification: certification(source.ContentRatings, release.Country), Note: release.Detail,
			})
		}
	}
	if len(record.Lifecycle.ReleaseEvents) == 0 {
		if date := normalizeDate(source.FirstRelease.Date); date != "" {
			record.Lifecycle.ReleaseEvents = append(record.Lifecycle.ReleaseEvents, moviedomain.ReleaseEvent{Country: strings.ToUpper(source.FirstRelease.Country), Type: "release", Date: date, Note: source.FirstRelease.Detail})
		}
	}
	if source.Runtime != nil && *source.Runtime > 0 {
		record.Measurements.RuntimeMinutes = source.Runtime
	}
	if source.Score != 0 {
		record.Measurements.Popularity = &source.Score
	}
	addCompanies := func(companies []company, role string) {
		for _, company := range companies {
			if company.ID > 0 && strings.TrimSpace(company.Name) != "" {
				record.Companies = append(record.Companies, moviedomain.Company{ProviderID: strconv.FormatInt(company.ID, 10), Name: company.Name, Role: role, Country: company.Country})
			}
		}
	}
	addCompanies(source.Companies.Studio, "studio")
	addCompanies(source.Companies.Production, "production")
	addCompanies(source.Companies.Distributor, "distributor")
	addCompanies(source.Companies.SpecialEffects, "special_effects")
	for _, studio := range source.Studios {
		if studio.ID > 0 && strings.TrimSpace(studio.Name) != "" {
			record.Companies = append(record.Companies, moviedomain.Company{ProviderID: strconv.FormatInt(studio.ID, 10), Name: studio.Name, Role: "studio"})
		}
	}
	for _, character := range source.Characters {
		if character.PeopleID < 1 || strings.TrimSpace(character.PersonName) == "" {
			continue
		}
		credit := moviedomain.Credit{ProviderPersonID: strconv.FormatInt(character.PeopleID, 10), DisplayName: character.PersonName, Order: character.Sort, ProfileURL: artworkURL(character.PersonImageURL)}
		if strings.EqualFold(character.PeopleType, "actor") || strings.EqualFold(character.PeopleType, "guest star") {
			credit.CreditType, credit.Character = "cast", character.Name
		} else {
			credit.CreditType, credit.Job = "crew", character.PeopleType
		}
		record.Credits = append(record.Credits, credit)
	}
	if image := artworkURL(source.Image); image != "" {
		record.Images = append(record.Images, moviedomain.Image{ProviderImageID: "primary:" + image, SourceURL: image, Class: "poster"})
	}
	for _, artwork := range source.Artworks {
		class := movieArtworkClass(artwork.Type)
		if class == "" || artwork.ID < 1 || artworkURL(artwork.Image) == "" {
			continue
		}
		record.Images = append(record.Images, moviedomain.Image{
			ProviderImageID: strconv.FormatInt(artwork.ID, 10), SourceURL: artworkURL(artwork.Image),
			Class: class, Language: normalizeArtworkLanguage(artwork.Language), Width: artwork.Width,
			Height: artwork.Height, ProviderScore: artwork.Score,
		})
	}
	for _, trailer := range source.Trailers {
		if parsed, err := url.Parse(trailer.URL); err == nil && parsed.Host != "" {
			record.Links = append(record.Links, moviedomain.Link{Kind: "trailer", Value: trailer.URL, Language: trailer.Language})
		}
	}
	return record, nil
}

func remoteCandidate(remote remoteID) (moviedomain.IdentityCandidate, bool) {
	value := strings.TrimSpace(remote.ID)
	if value == "" {
		return moviedomain.IdentityCandidate{}, false
	}
	provider, namespace := "", ""
	switch remote.Type {
	case 2:
		provider, namespace = "imdb", "title"
	case 12:
		provider, namespace = "tmdb", "movie"
	case 18:
		provider, namespace, value = "wikidata", "item", strings.ToUpper(value)
	case 23:
		provider, namespace = "anidb", "anime"
	}
	if provider == "" {
		return moviedomain.IdentityCandidate{}, false
	}
	return moviedomain.IdentityCandidate{Provider: provider, Namespace: namespace, NormalizedValue: value, Confidence: 1, Evidence: "tvdb.remote_ids"}, true
}

func movieArtworkClass(artworkType int) string {
	return map[int]string{13: "banner", 14: "poster", 15: "backdrop", 25: "logo"}[artworkType]
}

func artworkURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://artworks.thetvdb.com/" + strings.TrimLeft(value, "/")
}

func normalizeArtworkLanguage(value string) string {
	if value == "00" {
		return ""
	}
	return value
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "released":
		return "released"
	case "in production", "announced", "post production":
		return "in_production"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", "_"))
	}
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		if _, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return value[:10]
		}
	}
	return ""
}

func certification(ratings []contentRating, country string) string {
	for _, rating := range ratings {
		if strings.EqualFold(rating.Country, country) {
			return rating.Name
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}
