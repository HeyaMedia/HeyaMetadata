package deezer

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

// NormalizeArtistTopTracks maps /artist/{id}/top. Deezer's per-track "rank" is
// a popularity score, not a position, so rank is the list order and the score
// rides along as the playcount-like metric Deezer exposes.
func NormalizeArtistTopTracks(body []byte) (TopTracksSnapshot, error) {
	var envelope struct {
		Data []struct {
			ID         int64  `json:"id"`
			Title      string `json:"title"`
			TitleShort string `json:"title_short"`
			Link       string `json:"link"`
			Rank       int64  `json:"rank"`
		} `json:"data"`
		Total int `json:"total"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return TopTracksSnapshot{}, fmt.Errorf("decode Deezer top tracks: %w", err)
	}
	if envelope.Error != nil {
		return TopTracksSnapshot{}, fmt.Errorf("Deezer top tracks: %s", envelope.Error.Message)
	}
	snapshot := TopTracksSnapshot{Total: envelope.Total}
	for index, track := range envelope.Data {
		title := strings.TrimSpace(track.TitleShort)
		if title == "" {
			title = strings.TrimSpace(track.Title)
		}
		if track.ID < 1 || title == "" {
			continue
		}
		snapshot.Tracks = append(snapshot.Tracks, artistdomain.TopTrack{
			Rank: index + 1, Title: title, ProviderTrackID: strconv.FormatInt(track.ID, 10),
			Playcount: track.Rank, URL: track.Link,
		})
	}
	if snapshot.Total == 0 {
		snapshot.Total = len(snapshot.Tracks)
	}
	return snapshot, nil
}

// NormalizeRelatedArtists maps /artist/{id}/related to similar-artist
// candidates ordered as Deezer returned them.
func NormalizeRelatedArtists(body []byte) ([]artistdomain.SimilarArtist, error) {
	var envelope struct {
		Data []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Link   string `json:"link"`
			NbFan  int64  `json:"nb_fan"`
			Radio  bool   `json:"radio"`
			Tracks int64  `json:"nb_album"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Deezer related artists: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("Deezer related artists: %s", envelope.Error.Message)
	}
	var similar []artistdomain.SimilarArtist
	for _, artist := range envelope.Data {
		name := strings.TrimSpace(artist.Name)
		if artist.ID < 1 || name == "" {
			continue
		}
		similar = append(similar, artistdomain.SimilarArtist{
			ProviderID: strconv.FormatInt(artist.ID, 10), Name: name, URL: artist.Link, Score: float64(artist.NbFan),
		})
	}
	return similar, nil
}
