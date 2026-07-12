package deezer

import "testing"

func TestTrackPreviewURLRequiresExpectedTrack(t *testing.T) {
	url, err := TrackPreviewURL([]byte(`{"id":42,"preview":"https://cdnt-preview.dzcdn.net/a.mp3?token=fresh"}`), "42")
	if err != nil || url == "" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if _, err := TrackPreviewURL([]byte(`{"id":41,"preview":"https://cdnt-preview.dzcdn.net/a.mp3"}`), "42"); err == nil {
		t.Fatal("accepted wrong track")
	}
}
