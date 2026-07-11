package fanart

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
)

func Normalize(body []byte, observationID string, observedAt time.Time) (moviedomain.NormalizedRecordV1, error) {
	var source movieResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("decode Fanart.tv movie: %w", err)
	}
	tmdbID, err := strconv.ParseInt(source.TMDBID, 10, 64)
	if err != nil || tmdbID < 1 {
		return moviedomain.NormalizedRecordV1{}, fmt.Errorf("Fanart.tv movie is missing a valid TMDB identity")
	}
	record := moviedomain.NormalizedRecordV1{
		ProviderRecord: moviedomain.ProviderRecord{
			Provider: "fanart", Namespace: "movie", Value: source.TMDBID,
			PrimaryObservationID: observationID, ObservedAt: observedAt,
			NormalizerVersion: moviedomain.FanartNormalizerVersion, SchemaVersion: moviedomain.NormalizedSchemaVersion,
		},
		IdentityCandidates: []moviedomain.IdentityCandidate{{
			Provider: "tmdb", Namespace: "movie", NormalizedValue: source.TMDBID,
			Confidence: 1, Evidence: "provider_record",
		}},
	}
	if strings.HasPrefix(source.IMDBID, "tt") {
		record.IdentityCandidates = append(record.IdentityCandidates, moviedomain.IdentityCandidate{
			Provider: "imdb", Namespace: "title", NormalizedValue: source.IMDBID,
			Confidence: 1, Evidence: "fanart_movie",
		})
	}
	appendImages := func(class string, values []image) {
		for _, value := range values {
			if imageURL := normalizeURL(value.URL); imageURL != "" {
				likes := parsePositiveInt(value.Likes)
				language := strings.TrimSpace(value.Lang)
				if language == "00" {
					language = ""
				}
				record.Images = append(record.Images, moviedomain.Image{
					ProviderImageID: value.ID, SourceURL: imageURL, Class: class,
					Width: parsePositiveInt(value.Width), Height: parsePositiveInt(value.Height),
					Language: language, Likes: likes, ProviderScore: float64(likes),
				})
			}
		}
	}
	appendImages("poster", source.MoviePosters)
	appendImages("backdrop", source.MovieBackgrounds)
	appendImages("logo", source.HDMovieLogos)
	appendImages("logo", source.MovieLogos)
	appendImages("banner", source.MovieBanners)
	appendImages("clearart", source.HDMovieClearArts)
	appendImages("clearart", source.MovieArts)
	appendImages("thumb", source.MovieThumbs)
	appendImages("disc", source.MovieDiscs)
	return record, nil
}

func normalizeURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.Scheme = "https"
	return parsed.String()
}

func parsePositiveInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	if parsed < 0 {
		return 0
	}
	return parsed
}
