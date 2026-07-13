package lastfm

import "testing"

func TestNormalizeArtistTopTracksPreservesRankAndAudienceEvidence(t *testing.T) {
	const artistMBID = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	const recordingMBID = "485bbe7f-d0f7-4ffe-8adb-0f1093dd2dbf"
	snapshot, err := NormalizeArtistTopTracks([]byte(`{"toptracks":{"track":[{"name":"Come Together","mbid":"`+recordingMBID+`","url":"https://www.last.fm/music/The+Beatles/_/Come+Together","playcount":"1234","listeners":"567","artist":{"mbid":"`+artistMBID+`"},"@attr":{"rank":"1"}},{"name":"Wrong artist","artist":{"mbid":"00000000-0000-4000-8000-000000000001"},"@attr":{"rank":"2"}}],"@attr":{"total":"50"}}}`), artistMBID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Total != 50 || len(snapshot.Tracks) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	track := snapshot.Tracks[0]
	if track.Rank != 1 || track.RecordingMBID != recordingMBID || track.Playcount != 1234 || track.Listeners != 567 {
		t.Fatalf("track=%+v", track)
	}
}

func TestNormalizeArtistTopTracksAcceptsExplicitEmptyResult(t *testing.T) {
	snapshot, err := NormalizeArtistTopTracks([]byte(`{"toptracks":{"track":[],"@attr":{"total":"0"}}}`), "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d")
	if err != nil || snapshot.Total != 0 || snapshot.Tracks == nil || len(snapshot.Tracks) != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
