package lastfm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

// NormalizeSimilarArtists maps artist.getSimilar, which returns a far larger
// neighborhood than the ~5 similar artists embedded in artist.getInfo.
func NormalizeSimilarArtists(body []byte) ([]artistdomain.SimilarArtist, error) {
	var envelope struct {
		SimilarArtists struct {
			Artists []struct {
				Name  string `json:"name"`
				MBID  string `json:"mbid"`
				Match string `json:"match"`
				URL   string `json:"url"`
			} `json:"artist"`
		} `json:"similarartists"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Last.fm similar artists: %w", err)
	}
	if envelope.Error != 0 {
		return nil, fmt.Errorf("Last.fm similar artists: %s", envelope.Message)
	}
	var similar []artistdomain.SimilarArtist
	for _, artist := range envelope.SimilarArtists.Artists {
		name := strings.TrimSpace(artist.Name)
		if name == "" {
			continue
		}
		candidate := artistdomain.SimilarArtist{Name: name, URL: artist.URL}
		if id := strings.ToLower(strings.TrimSpace(artist.MBID)); mbidPattern.MatchString(id) {
			candidate.ProviderID = id
		}
		if match, err := strconv.ParseFloat(artist.Match, 64); err == nil {
			candidate.Score = match
		}
		similar = append(similar, candidate)
	}
	return similar, nil
}
