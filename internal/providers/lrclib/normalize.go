package lrclib

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const NormalizerVersion = "lrclib-lyrics/v1"

type Evidence struct {
	Provider             string    `json:"provider"`
	ProviderRecordID     string    `json:"provider_record_id"`
	PrimaryObservationID string    `json:"primary_observation_id"`
	ObservedAt           time.Time `json:"observed_at"`
	NormalizerVersion    string    `json:"normalizer_version"`
	TrackName            string    `json:"track_name"`
	ArtistName           string    `json:"artist_name"`
	AlbumName            string    `json:"album_name"`
	DurationMS           int64     `json:"duration_ms"`
	Instrumental         bool      `json:"instrumental"`
	PlainLyrics          string    `json:"plain_lyrics,omitempty"`
	SyncedLyrics         string    `json:"synced_lyrics,omitempty"`
	ContentChecksum      string    `json:"content_checksum"`
}

func Normalize(body []byte, observationID string, observedAt time.Time, requested Signature) (Evidence, error) {
	var source struct {
		ID           int64   `json:"id"`
		TrackName    string  `json:"trackName"`
		ArtistName   string  `json:"artistName"`
		AlbumName    string  `json:"albumName"`
		Duration     float64 `json:"duration"`
		Instrumental bool    `json:"instrumental"`
		PlainLyrics  *string `json:"plainLyrics"`
		SyncedLyrics *string `json:"syncedLyrics"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Evidence{}, fmt.Errorf("decode LRCLIB lyrics: %w", err)
	}
	if source.ID < 1 || strings.TrimSpace(source.TrackName) == "" || strings.TrimSpace(source.ArtistName) == "" {
		return Evidence{}, fmt.Errorf("LRCLIB response is missing identity")
	}
	difference := source.Duration - float64(requested.Duration)
	// LRCLIB selects with a nominal two-second window, but its stored durations
	// can be fractional while the request is an integer. Three seconds safely
	// covers the rounding edge without accepting a materially different edit.
	if difference < -3 || difference > 3 {
		return Evidence{}, fmt.Errorf("LRCLIB response duration differs from requested signature")
	}
	plain, synced := "", ""
	if source.PlainLyrics != nil {
		plain = strings.TrimSpace(*source.PlainLyrics)
	}
	if source.SyncedLyrics != nil {
		synced = strings.TrimSpace(*source.SyncedLyrics)
	}
	if !source.Instrumental && plain == "" && synced == "" {
		return Evidence{}, fmt.Errorf("LRCLIB response has no lyrics and is not instrumental")
	}
	sum := sha256.Sum256([]byte(plain + "\x00" + synced + "\x00" + strconvBool(source.Instrumental)))
	return Evidence{
		Provider: "lrclib", ProviderRecordID: fmt.Sprintf("%d", source.ID), PrimaryObservationID: observationID,
		ObservedAt: observedAt, NormalizerVersion: NormalizerVersion, TrackName: strings.TrimSpace(source.TrackName),
		ArtistName: strings.TrimSpace(source.ArtistName), AlbumName: strings.TrimSpace(source.AlbumName),
		DurationMS: int64(source.Duration * 1000), Instrumental: source.Instrumental,
		PlainLyrics: plain, SyncedLyrics: synced, ContentChecksum: hex.EncodeToString(sum[:]),
	}, nil
}

func strconvBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
