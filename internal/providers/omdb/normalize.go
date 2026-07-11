package omdb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
)

func Normalize(body []byte, observationID string, observedAt time.Time) (moviedomain.NormalizedRecordV1, error) {
	var source response
	if err := json.Unmarshal(body, &source); err != nil {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("decode OMDb movie: %w", err)
	}
	if strings.EqualFold(source.Response, "False") {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("OMDb response: %s", source.Error)
	}
	if !imdbTitlePattern.MatchString(source.IMDBID) || strings.TrimSpace(source.Title) == "" {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("OMDb movie is missing IMDb identity or title")
	}
	record := moviedomain.NormalizedRecordV1{
		ProviderRecord: moviedomain.ProviderRecord{
			Provider: "omdb", Namespace: "imdb_title", Value: source.IMDBID,
			PrimaryObservationID: observationID, ObservedAt: observedAt,
			NormalizerVersion: moviedomain.OMDBNormalizerVersion, SchemaVersion: moviedomain.NormalizedSchemaVersion,
		},
		IdentityCandidates: []moviedomain.IdentityCandidate{{
			Provider: "imdb", Namespace: "title", NormalizedValue: source.IMDBID,
			Confidence: 1, Evidence: "provider_record",
		}},
		Titles:         []moviedomain.LocalizedText{{Value: strings.TrimSpace(source.Title), Type: "display"}},
		Classification: moviedomain.Classification{ProviderMediaType: strings.ToLower(source.Type)},
	}
	if value := meaningful(source.Plot); value != "" {
		record.Descriptions = append(record.Descriptions, moviedomain.LocalizedText{Value: value, Language: "en", Type: "overview"})
	}
	if released, err := time.Parse("02 Jan 2006", meaningful(source.Released)); err == nil {
		record.Lifecycle.ReleaseEvents = append(record.Lifecycle.ReleaseEvents, moviedomain.ReleaseEvent{Type: "release", Date: released.Format("2006-01-02")})
	}
	if minutes := parseRuntime(source.Runtime); minutes > 0 {
		record.Measurements.RuntimeMinutes = &minutes
	}
	if value, err := strconv.ParseFloat(meaningful(source.IMDBRating), 64); err == nil && value >= 0 && value <= 10 {
		record.Ratings = append(record.Ratings, moviedomain.Rating{
			System: "imdb", Value: value, ScaleMin: 0, ScaleMax: 10,
			Votes: parseInteger(source.IMDBVotes), RawValue: source.IMDBRating,
		})
	}
	seenMetacritic := false
	for _, rating := range source.Ratings {
		switch strings.ToLower(strings.TrimSpace(rating.Source)) {
		case "rotten tomatoes":
			if value, ok := parseScaledRating(rating.Value, "%", 100); ok {
				record.Ratings = append(record.Ratings, moviedomain.Rating{System: "rotten_tomatoes", Value: value, ScaleMin: 0, ScaleMax: 100, RawValue: rating.Value})
			}
		case "metacritic":
			if value, ok := parseScaledRating(rating.Value, "/100", 100); ok {
				record.Ratings = append(record.Ratings, moviedomain.Rating{System: "metacritic", Value: value, ScaleMin: 0, ScaleMax: 100, RawValue: rating.Value})
				seenMetacritic = true
			}
		}
	}
	if !seenMetacritic {
		if value, err := strconv.ParseFloat(meaningful(source.Metascore), 64); err == nil && value >= 0 && value <= 100 {
			record.Ratings = append(record.Ratings, moviedomain.Rating{System: "metacritic", Value: value, ScaleMin: 0, ScaleMax: 100, RawValue: source.Metascore})
		}
	}
	if website := meaningful(source.Website); website != "" {
		record.Links = append(record.Links, moviedomain.Link{Kind: "homepage", Value: website})
	}
	return record, nil
}

func meaningful(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "N/A") {
		return ""
	}
	return value
}

func parseRuntime(value string) int {
	fields := strings.Fields(meaningful(value))
	if len(fields) == 0 {
		return 0
	}
	minutes, _ := strconv.Atoi(fields[0])
	return minutes
}

func parseInteger(value string) int {
	value = strings.NewReplacer(",", "", ".", "", " ", "").Replace(meaningful(value))
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func parseScaledRating(raw, suffix string, max float64) (float64, bool) {
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), suffix))
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil && parsed >= 0 && parsed <= max
}
