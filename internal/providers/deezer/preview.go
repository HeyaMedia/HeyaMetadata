package deezer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TrackPreviewURL extracts a just-issued preview URL from Deezer's track
// endpoint. The caller must consume it immediately; its query token expires.
func TrackPreviewURL(body []byte, expectedID string) (string, error) {
	var value struct {
		ID      json.Number `json:"id"`
		Preview string      `json:"preview"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", fmt.Errorf("decode Deezer track preview: %w", err)
	}
	if value.Error != nil {
		return "", fmt.Errorf("Deezer track preview: %s", value.Error.Message)
	}
	if string(value.ID) != expectedID || strings.TrimSpace(value.Preview) == "" {
		return "", fmt.Errorf("Deezer track %s has no preview", expectedID)
	}
	return value.Preview, nil
}
