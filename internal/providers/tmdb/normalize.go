package tmdb

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
)

const imageBaseURL = "https://image.tmdb.org/t/p/original"

var imdbIDPattern = regexp.MustCompile(`^tt[0-9]+$`)
var wikidataIDPattern = regexp.MustCompile(`^Q[0-9]+$`)

func Normalize(detailBody []byte, collectionBody []byte, primaryObservationID string, supportingObservationIDs []string, observedAt time.Time, locale string) (moviedomain.NormalizedRecordV1, error) {
	var detail MovieDetail
	if err := json.Unmarshal(detailBody, &detail); err != nil {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("decode TMDB movie detail: %w", err)
	}
	if detail.ID < 1 || strings.TrimSpace(detail.Title) == "" {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("TMDB movie detail is missing id or title")
	}
	record := moviedomain.NormalizedRecordV1{
		ProviderRecord: moviedomain.ProviderRecord{
			Provider: "tmdb", Namespace: "movie", Value: strconv.FormatInt(detail.ID, 10),
			PrimaryObservationID: primaryObservationID, SupportingObservationIDs: supportingObservationIDs,
			ObservedAt: observedAt, NormalizerVersion: moviedomain.TMDBNormalizerVersion,
			SchemaVersion: moviedomain.NormalizedSchemaVersion,
		},
		Classification: moviedomain.Classification{
			ProviderMediaType: "movie", OriginalLanguage: detail.OriginalLanguage,
		},
		Lifecycle: moviedomain.Lifecycle{RawStatus: detail.Status, NormalizedStatus: normalizeStatus(detail.Status)},
	}
	record.IdentityCandidates = append(record.IdentityCandidates, moviedomain.IdentityCandidate{
		Provider: "tmdb", Namespace: "movie", NormalizedValue: strconv.FormatInt(detail.ID, 10), Confidence: 1, Evidence: "provider_record",
	})
	imdbID := firstNonEmpty(detail.ExternalIDs.IMDbID, detail.IMDbID)
	imdbID = strings.ToLower(imdbID)
	if imdbIDPattern.MatchString(imdbID) {
		record.IdentityCandidates = append(record.IdentityCandidates, moviedomain.IdentityCandidate{
			Provider: "imdb", Namespace: "title", NormalizedValue: imdbID, Confidence: 1, Evidence: "tmdb.external_ids",
		})
	}
	wikidataID := strings.ToUpper(detail.ExternalIDs.WikidataID)
	if wikidataIDPattern.MatchString(wikidataID) {
		record.IdentityCandidates = append(record.IdentityCandidates, moviedomain.IdentityCandidate{
			Provider: "wikidata", Namespace: "item", NormalizedValue: wikidataID, Confidence: 1, Evidence: "tmdb.external_ids",
		})
	}
	record.Titles = append(record.Titles,
		moviedomain.LocalizedText{Value: detail.Title, Language: languageFromLocale(locale), Type: "display"},
	)
	if detail.OriginalTitle != "" {
		record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: detail.OriginalTitle, Language: detail.OriginalLanguage, Type: "original"})
	}
	if detail.Overview != "" {
		record.Descriptions = append(record.Descriptions, moviedomain.LocalizedText{Value: detail.Overview, Language: languageFromLocale(locale), Type: "overview"})
	}
	if detail.Tagline != "" {
		record.Taglines = append(record.Taglines, moviedomain.LocalizedText{Value: detail.Tagline, Language: languageFromLocale(locale), Type: "tagline"})
	}
	for _, alternative := range detail.AlternativeTitles.Titles {
		if alternative.Title != "" {
			record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: alternative.Title, Country: alternative.Country, Type: "alternative"})
		}
	}
	for _, translation := range detail.Translations.Translations {
		if translation.Data.Title != "" {
			record.Titles = append(record.Titles, moviedomain.LocalizedText{Value: translation.Data.Title, Language: translation.Language, Country: translation.Country, Type: "translated"})
		}
		if translation.Data.Overview != "" {
			record.Descriptions = append(record.Descriptions, moviedomain.LocalizedText{Value: translation.Data.Overview, Language: translation.Language, Country: translation.Country, Type: "overview"})
		}
		if translation.Data.Tagline != "" {
			record.Taglines = append(record.Taglines, moviedomain.LocalizedText{Value: translation.Data.Tagline, Language: translation.Language, Country: translation.Country, Type: "tagline"})
		}
	}
	for _, genre := range detail.Genres {
		record.Classification.Genres = append(record.Classification.Genres, genre.Name)
		if genre.ID == 16 {
			record.Classification.AnimationEvidence = true
		}
	}
	keywords := append(detail.Keywords.Keywords, detail.Keywords.Results...)
	for _, keyword := range keywords {
		record.Classification.Keywords = append(record.Classification.Keywords, keyword.Name)
	}
	record.Classification.Countries = append(record.Classification.Countries, detail.OriginCountry...)
	for _, country := range detail.ProductionCountries {
		record.Classification.Countries = appendUnique(record.Classification.Countries, country.Code)
	}
	for _, language := range detail.SpokenLanguages {
		record.Classification.SpokenLanguages = appendUnique(record.Classification.SpokenLanguages, language.Code)
	}
	for _, releaseGroup := range detail.ReleaseDates.Results {
		for _, release := range releaseGroup.Dates {
			record.Lifecycle.ReleaseEvents = append(record.Lifecycle.ReleaseEvents, moviedomain.ReleaseEvent{
				Country: releaseGroup.Country, Type: releaseType(release.Type), Date: release.Date,
				Certification: release.Certification, Note: release.Note,
			})
		}
	}
	if len(record.Lifecycle.ReleaseEvents) == 0 && detail.ReleaseDate != "" {
		record.Lifecycle.ReleaseEvents = append(record.Lifecycle.ReleaseEvents, moviedomain.ReleaseEvent{Type: "provider_primary", Date: detail.ReleaseDate})
	}
	if detail.Runtime > 0 {
		runtime := detail.Runtime
		record.Measurements.RuntimeMinutes = &runtime
	}
	if detail.Budget > 0 {
		record.Measurements.Budget = &moviedomain.Money{Amount: detail.Budget, Currency: "USD", CurrencyBasis: "tmdb_documented_usd"}
	}
	if detail.Revenue > 0 {
		record.Measurements.Revenue = &moviedomain.Money{Amount: detail.Revenue, Currency: "USD", CurrencyBasis: "tmdb_documented_usd"}
	}
	record.Measurements.Popularity = &detail.Popularity
	if detail.VoteCount > 0 {
		record.Ratings = append(record.Ratings, moviedomain.Rating{System: "tmdb", Value: detail.VoteAverage, ScaleMin: 0, ScaleMax: 10, Votes: detail.VoteCount, RawValue: strconv.FormatFloat(detail.VoteAverage, 'f', -1, 64)})
	}
	if detail.Homepage != "" {
		record.Links = append(record.Links, moviedomain.Link{Kind: "homepage", Value: detail.Homepage})
	}
	for _, social := range []struct{ kind, value string }{{"facebook", detail.ExternalIDs.FacebookID}, {"instagram", detail.ExternalIDs.InstagramID}, {"twitter", detail.ExternalIDs.TwitterID}} {
		if social.value != "" {
			record.Links = append(record.Links, moviedomain.Link{Kind: social.kind, Value: social.value})
		}
	}
	for _, video := range detail.Videos.Results {
		record.Videos = append(record.Videos, moviedomain.Video{Host: video.Site, Key: video.Key, Type: video.Type, Name: video.Name, Language: video.Language, Country: video.Country, Official: video.Official, PublishedAt: video.PublishedAt})
	}
	for _, company := range detail.ProductionCompanies {
		record.Companies = append(record.Companies, moviedomain.Company{ProviderID: strconv.FormatInt(company.ID, 10), Name: company.Name, Role: "production", Country: company.OriginCountry, LogoURL: imageURL(company.LogoPath)})
	}
	for _, cast := range detail.Credits.Cast {
		record.Credits = append(record.Credits, moviedomain.Credit{ProviderPersonID: strconv.FormatInt(cast.ID, 10), DisplayName: cast.Name, CreditType: "cast", Character: cast.Character, Order: cast.Order, ProfileURL: imageURL(cast.ProfilePath)})
	}
	for index, crew := range detail.Credits.Crew {
		record.Credits = append(record.Credits, moviedomain.Credit{ProviderPersonID: strconv.FormatInt(crew.ID, 10), DisplayName: crew.Name, CreditType: "crew", Department: crew.Department, Job: crew.Job, Order: index, ProfileURL: imageURL(crew.ProfilePath)})
	}
	appendImages := func(class string, images []image) {
		for _, candidate := range images {
			language := ""
			if candidate.Language != nil {
				language = *candidate.Language
			}
			record.Images = append(record.Images, moviedomain.Image{ProviderImageID: candidate.FilePath, SourceURL: imageURL(candidate.FilePath), Class: class, Width: candidate.Width, Height: candidate.Height, Language: language, ProviderScore: candidate.VoteAverage, Likes: candidate.VoteCount})
		}
	}
	appendImages("poster", detail.Images.Posters)
	appendImages("backdrop", detail.Images.Backdrops)
	appendImages("logo", detail.Images.Logos)
	for _, recommendation := range detail.Recommendations.Results {
		record.Recommendations = append(record.Recommendations, moviedomain.Recommendation{ProviderTargetID: strconv.FormatInt(recommendation.ID, 10), Title: recommendation.Title, Year: year(recommendation.ReleaseDate), ImageURL: imageURL(recommendation.PosterPath), ProviderScore: recommendation.Popularity})
	}
	if len(collectionBody) > 0 {
		collection, err := normalizeCollection(collectionBody)
		if err != nil {
			record.Warnings = append(record.Warnings, err.Error())
			record.PartialFailure = true
		} else {
			record.Collection = collection
		}
	} else if detail.Collection != nil {
		record.Collection = &moviedomain.Collection{ProviderID: strconv.FormatInt(detail.Collection.ID, 10), Name: detail.Collection.Name}
	}
	return record, nil
}

func normalizeCollection(body []byte) (*moviedomain.Collection, error) {
	var source CollectionDetail
	if err := json.Unmarshal(body, &source); err != nil {
		return nil, fmt.Errorf("decode TMDB collection: %w", err)
	}
	collection := &moviedomain.Collection{ProviderID: strconv.FormatInt(source.ID, 10), Name: source.Name, Overview: source.Overview}
	for index, part := range source.Parts {
		collection.Members = append(collection.Members, moviedomain.CollectionMember{ProviderID: strconv.FormatInt(part.ID, 10), Title: part.Title, Year: year(part.ReleaseDate), ImageURL: imageURL(part.PosterPath), Order: index})
	}
	for _, candidate := range source.Posters {
		collection.Images = append(collection.Images, moviedomain.Image{ProviderImageID: candidate.FilePath, SourceURL: imageURL(candidate.FilePath), Class: "poster", Width: candidate.Width, Height: candidate.Height, ProviderScore: candidate.VoteAverage})
	}
	for _, candidate := range source.Backdrops {
		collection.Images = append(collection.Images, moviedomain.Image{ProviderImageID: candidate.FilePath, SourceURL: imageURL(candidate.FilePath), Class: "backdrop", Width: candidate.Width, Height: candidate.Height, ProviderScore: candidate.VoteAverage})
	}
	return collection, nil
}

func releaseType(value int) string {
	return map[int]string{1: "premiere", 2: "limited_theatrical", 3: "theatrical", 4: "digital", 5: "physical", 6: "television"}[value]
}

func normalizeStatus(value string) string {
	switch strings.ToLower(value) {
	case "released":
		return "released"
	case "post production":
		return "post_production"
	case "in production":
		return "in_production"
	case "planned", "rumored":
		return "planned"
	case "canceled", "cancelled":
		return "cancelled"
	default:
		return strings.ToLower(strings.ReplaceAll(value, " ", "_"))
	}
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return imageBaseURL + path
}
func year(value string) int {
	if len(value) < 4 {
		return 0
	}
	result, _ := strconv.Atoi(value[:4])
	return result
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
func languageFromLocale(locale string) string {
	if len(locale) >= 2 {
		return locale[:2]
	}
	return locale
}
