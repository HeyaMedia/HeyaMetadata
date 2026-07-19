package artists

import (
	"errors"
	"net/http"
	"testing"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestSameTopTrackContentIgnoresInputOrdering(t *testing.T) {
	left := []artistdomain.TopTrack{
		{Rank: 1, Title: "First", RecordingMBID: "one", Playcount: 10},
		{Rank: 2, Title: "Second", RecordingMBID: "two", Playcount: 5},
	}
	right := []artistdomain.TopTrack{left[1], left[0]}
	if !sameTopTrackContent(left, right) {
		t.Fatal("equivalent rankings were treated as changed")
	}
	right[0].Playcount++
	if sameTopTrackContent(left, right) {
		t.Fatal("audience evidence change was treated as identical")
	}
}

func TestTopTrackFailureClass(t *testing.T) {
	if got := topTrackFailureClass(&providers.StatusError{Provider: "lastfm", StatusCode: http.StatusTooManyRequests}); got != "rate_limited" {
		t.Fatalf("rate limit class = %q", got)
	}
	if got := topTrackFailureClass(&providers.StatusError{Provider: "lastfm", StatusCode: http.StatusNotFound}); got != "not_found" {
		t.Fatalf("not found class = %q", got)
	}
	if got := topTrackFailureClass(errors.New("network down")); got != "transient" {
		t.Fatalf("transient class = %q", got)
	}
}
