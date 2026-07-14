package release

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrackDocumentExposesCanonicalRecordingID(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(TrackDocument{ID: "track", RecordingEntityID: "recording", LyricsAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"recording_entity_id":"recording"`) {
		t.Fatalf("track JSON: %s", body)
	}
	if !strings.Contains(string(body), `"lyrics_available":true`) {
		t.Fatalf("track JSON: %s", body)
	}
}
