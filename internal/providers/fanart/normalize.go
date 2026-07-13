package fanart

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
)

const TVNormalizerVersion = "fanart-tv/v1"

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

// NormalizeTV retains every TV and season artwork class exposed by Fanart.tv.
// The record is intentionally metadata-light: it contributes artwork evidence
// to an existing TVDB-identified canonical show or anime entity.
func NormalizeTV(body []byte, observationID string, observedAt time.Time, kind string) (episodic.NormalizedRecord, error) {
	var source tvResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return episodic.NormalizedRecord{}, fmt.Errorf("decode Fanart.tv TV series: %w", err)
	}
	tvdbID, err := strconv.ParseInt(source.TVDBID, 10, 64)
	if err != nil || tvdbID < 1 {
		return episodic.NormalizedRecord{}, fmt.Errorf("Fanart.tv TV series is missing a valid TVDB identity")
	}
	if kind != "tv_show" && kind != "anime" {
		return episodic.NormalizedRecord{}, fmt.Errorf("unsupported Fanart.tv episodic kind %q", kind)
	}
	record := episodic.NormalizedRecord{
		SchemaVersion: 1, Kind: kind, Provider: "fanart", Namespace: "series", ProviderID: source.TVDBID,
		PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: TVNormalizerVersion,
		ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "series", Value: source.TVDBID}},
	}
	if name := strings.TrimSpace(source.Name); name != "" {
		record.Titles = []episodic.Title{{Value: name, Type: "provider"}}
	}
	appendRootImages := func(class, prefix string, values []image) {
		for _, value := range values {
			if item, ok := episodicImage(class, prefix, value); ok {
				record.Images = append(record.Images, item)
			}
		}
	}
	appendRootImages("poster", "tvposter", source.TVPosters)
	appendRootImages("backdrop", "showbackground", source.ShowBackgrounds)
	appendRootImages("logo", "hdtvlogo", source.HDTVLogos)
	appendRootImages("logo", "clearlogo", source.ClearLogos)
	appendRootImages("banner", "tvbanner", source.TVBanners)
	appendRootImages("clearart", "hdclearart", source.HDClearArts)
	appendRootImages("clearart", "clearart", source.ClearArts)
	appendRootImages("thumb", "tvthumb", source.TVThumbs)
	appendRootImages("characterart", "characterart", source.CharacterArts)

	seasons := map[int]*episodic.Season{}
	appendSeasonImages := func(class, prefix string, values []seasonImage) {
		for _, value := range values {
			item, ok := episodicImage(class, prefix, value.image)
			if !ok {
				continue
			}
			seasonNumber, parseErr := strconv.Atoi(strings.TrimSpace(value.Season))
			if parseErr != nil || seasonNumber < 0 {
				// Fanart uses a null season for artwork intended to represent the
				// entire series. Preserve it as show-level artwork.
				record.Images = append(record.Images, item)
				continue
			}
			season := seasons[seasonNumber]
			if season == nil {
				season = &episodic.Season{Number: seasonNumber}
				seasons[seasonNumber] = season
			}
			season.Images = append(season.Images, item)
		}
	}
	appendSeasonImages("poster", "seasonposter", source.SeasonPosters)
	appendSeasonImages("banner", "seasonbanner", source.SeasonBanners)
	appendSeasonImages("thumb", "seasonthumb", source.SeasonThumbs)
	for seasonNumber := 0; seasonNumber <= 1000; seasonNumber++ {
		if season := seasons[seasonNumber]; season != nil {
			record.Seasons = append(record.Seasons, *season)
		}
	}
	return record, nil
}

func episodicImage(class, prefix string, value image) (episodic.Image, bool) {
	imageURL := normalizeURL(value.URL)
	if imageURL == "" {
		return episodic.Image{}, false
	}
	language := strings.TrimSpace(value.Lang)
	if language == "00" {
		language = ""
	}
	likes := parsePositiveInt(value.Likes)
	return episodic.Image{
		Provider: "fanart", ProviderID: prefix + ":" + value.ID, URL: imageURL, Class: class,
		Language: language, Width: parsePositiveInt(value.Width), Height: parsePositiveInt(value.Height),
		ProviderScore: float64(likes),
	}, true
}

func NormalizeMusicArtist(body []byte, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var source musicResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Fanart.tv music artist: %w", err)
	}
	mbid := strings.ToLower(strings.TrimSpace(source.MBID))
	if !mbidPattern.MatchString(mbid) {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Fanart.tv music artist is missing a valid MusicBrainz identity")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "fanart", Namespace: "artist", Value: mbid, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.FanartNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: mbid, Confidence: 1, Evidence: "fanart_music_mbid"}},
	}
	if name := strings.TrimSpace(source.Name); name != "" {
		record.Names = []artistdomain.Name{{Value: name, Type: "provider"}}
	}
	appendImages := func(class, prefix string, values []image) {
		for _, value := range values {
			imageURL := normalizeURL(value.URL)
			if imageURL == "" {
				continue
			}
			language := strings.TrimSpace(value.Lang)
			if language == "00" {
				language = ""
			}
			record.Images = append(record.Images, artistdomain.Image{ProviderImageID: prefix + ":" + value.ID, SourceURL: imageURL, Class: class, Language: language, Width: parsePositiveInt(value.Width), Height: parsePositiveInt(value.Height), ProviderScore: float64(parsePositiveInt(value.Likes))})
		}
	}
	appendImages("backdrop", "artistbackground", source.ArtistBackgrounds)
	appendImages("profile", "artistthumb", source.ArtistThumbs)
	appendImages("logo", "hdartistlogo", source.HDArtistLogos)
	appendImages("logo", "hdmusiclogo", source.HDMusicLogos)
	appendImages("logo", "musiclogo", source.MusicLogos)
	appendImages("banner", "musicbanner", source.MusicBanners)
	return record, nil
}

func NormalizeMusicReleaseGroup(body []byte, releaseGroupMBID, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	var source musicResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Fanart.tv music release group: %w", err)
	}
	releaseGroupMBID = strings.ToLower(strings.TrimSpace(releaseGroupMBID))
	if !mbidPattern.MatchString(releaseGroupMBID) {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Fanart.tv music release group requires a valid MusicBrainz identity")
	}
	record := rgdomain.NormalizedRecordV1{
		ProviderRecord:     rgdomain.ProviderRecord{Provider: "fanart", Namespace: "release_group", Value: releaseGroupMBID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.FanartNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion},
		IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "release_group", NormalizedValue: releaseGroupMBID, Confidence: 1, Evidence: "fanart_music_release_group"}},
	}
	for _, album := range source.Albums {
		if !strings.EqualFold(strings.TrimSpace(album.ReleaseGroupID), releaseGroupMBID) {
			continue
		}
		appendImages := func(class, prefix string, values []image) {
			for _, value := range values {
				imageURL := normalizeURL(value.URL)
				if imageURL == "" {
					continue
				}
				record.Images = append(record.Images, rgdomain.Image{ProviderImageID: prefix + ":" + value.ID, SourceURL: imageURL, Class: class, Width: parsePositiveInt(value.Width), Height: parsePositiveInt(value.Height), ProviderScore: float64(parsePositiveInt(value.Likes))})
			}
		}
		appendImages("cover", "albumcover", album.AlbumCovers)
		appendImages("disc", "cdart", album.CDArt)
		break
	}
	return record, nil
}
