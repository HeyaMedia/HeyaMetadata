package audiodb

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

// NormalizeArtistMusicVideos maps mvid-mb.php entries. Every entry echoes the
// MusicBrainz artist ID; entries that do not match the expected artist are
// dropped rather than failing the batch.
func NormalizeArtistMusicVideos(body []byte, expectedMBID string, _ time.Time) ([]artistdomain.MusicVideo, error) {
	var envelope struct {
		Videos []struct {
			TrackID       string `json:"idTrack"`
			Track         string `json:"strTrack"`
			MusicVid      string `json:"strMusicVid"`
			Description   string `json:"strDescription"`
			MusicBrainzID string `json:"strMusicBrainzArtistID"`
		} `json:"mvids"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode TheAudioDB music videos: %w", err)
	}
	expectedMBID = strings.ToLower(strings.TrimSpace(expectedMBID))
	var videos []artistdomain.MusicVideo
	for _, video := range envelope.Videos {
		title := strings.TrimSpace(video.Track)
		videoURL := normalizeLinkURL(video.MusicVid)
		if title == "" || videoURL == "" {
			continue
		}
		if mbid := strings.ToLower(strings.TrimSpace(video.MusicBrainzID)); mbid != "" && mbid != expectedMBID {
			continue
		}
		videos = append(videos, artistdomain.MusicVideo{
			ProviderVideoID: strings.TrimSpace(video.TrackID),
			TrackTitle:      title,
			URL:             videoURL,
			Description:     strings.TrimSpace(video.Description),
		})
	}
	return videos, nil
}
