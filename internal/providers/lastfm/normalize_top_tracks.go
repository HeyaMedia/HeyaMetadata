package lastfm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

type TopTracksSnapshot struct {
	Tracks []artistdomain.TopTrack
	Total  int
}

func NormalizeArtistTopTracks(body []byte, expectedArtistMBID string) (TopTracksSnapshot, error) {
	var envelope struct {
		TopTracks struct {
			Tracks []struct {
				Name      string `json:"name"`
				MBID      string `json:"mbid"`
				URL       string `json:"url"`
				Playcount string `json:"playcount"`
				Listeners string `json:"listeners"`
				Artist    struct {
					MBID string `json:"mbid"`
				} `json:"artist"`
				Attributes struct {
					Rank string `json:"rank"`
				} `json:"@attr"`
			} `json:"track"`
			Attributes struct {
				Total string `json:"total"`
			} `json:"@attr"`
		} `json:"toptracks"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return TopTracksSnapshot{}, fmt.Errorf("decode Last.fm artist top tracks: %w", err)
	}
	if envelope.Error != 0 {
		return TopTracksSnapshot{}, fmt.Errorf("Last.fm artist top tracks: %s", envelope.Message)
	}
	expectedArtistMBID = strings.ToLower(strings.TrimSpace(expectedArtistMBID))
	if !mbidPattern.MatchString(expectedArtistMBID) {
		return TopTracksSnapshot{}, fmt.Errorf("invalid expected MusicBrainz artist identity")
	}
	snapshot := TopTracksSnapshot{Tracks: []artistdomain.TopTrack{}}
	snapshot.Total, _ = strconv.Atoi(envelope.TopTracks.Attributes.Total)
	seenRank := map[int]bool{}
	for index, source := range envelope.TopTracks.Tracks {
		artistMBID := strings.ToLower(strings.TrimSpace(source.Artist.MBID))
		if artistMBID != "" && artistMBID != expectedArtistMBID {
			continue
		}
		rank, _ := strconv.Atoi(source.Attributes.Rank)
		if rank < 1 {
			rank = index + 1
		}
		if seenRank[rank] || strings.TrimSpace(source.Name) == "" {
			continue
		}
		seenRank[rank] = true
		track := artistdomain.TopTrack{
			Rank:      rank,
			Title:     strings.TrimSpace(source.Name),
			URL:       strings.TrimSpace(source.URL),
			Playcount: parseInt64(source.Playcount),
			Listeners: parseInt64(source.Listeners),
		}
		if mbid := strings.ToLower(strings.TrimSpace(source.MBID)); mbidPattern.MatchString(mbid) {
			track.RecordingMBID = mbid
		}
		snapshot.Tracks = append(snapshot.Tracks, track)
	}
	if snapshot.Total < len(snapshot.Tracks) {
		snapshot.Total = len(snapshot.Tracks)
	}
	return snapshot, nil
}

func parseInt64(value string) int64 {
	result, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return result
}
