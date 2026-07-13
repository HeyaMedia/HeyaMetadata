package lastfm

import "testing"

func TestNormalizeArtistTopTracksPreservesRankAndAudienceEvidence(t *testing.T) {
	const artistMBID = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	const recordingMBID = "485bbe7f-d0f7-4ffe-8adb-0f1093dd2dbf"
	snapshot, err := NormalizeArtistTopTracks([]byte(`{"toptracks":{"track":[{"name":"Come Together","mbid":"`+recordingMBID+`","url":"https://www.last.fm/music/The+Beatles/_/Come+Together","playcount":"1234","listeners":"567","artist":{"name":"The Beatles","mbid":"`+artistMBID+`"},"@attr":{"rank":"1"}},{"name":"Wrong artist","artist":{"name":"Wrong","mbid":"00000000-0000-4000-8000-000000000001"},"@attr":{"rank":"2"}}],"@attr":{"artist":"The Beatles","total":"50"}}}`), artistMBID, []string{"The Beatles"})
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
	snapshot, err := NormalizeArtistTopTracks([]byte(`{"toptracks":{"track":[],"@attr":{"total":"0"}}}`), "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d", []string{"The Beatles"})
	if err != nil || snapshot.Total != 0 || snapshot.Tracks == nil || len(snapshot.Tracks) != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestNormalizeArtistTopTracksRetainsNameScopedAggregateWithoutRecordingClaims(t *testing.T) {
	const expectedArtist = "e134b52f-2e9e-4734-9bc3-bea9648d1fa1"
	const wrongArtist = "ca195d97-30d9-4870-8a52-7fd3c2e175c3"
	const recording = "5d992918-e00d-420a-85cd-04dd6e262a00"
	snapshot, err := NormalizeArtistTopTracks([]byte(`{"toptracks":{"track":[{"name":"Usseewa","mbid":"`+recording+`","artist":{"name":"Ado","mbid":"`+wrongArtist+`"},"@attr":{"rank":"1"}}],"@attr":{"artist":"Ado","total":"1"}}}`), expectedArtist, []string{"Ado"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.NameScoped || len(snapshot.Tracks) != 1 || snapshot.Tracks[0].RecordingMBID != "" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
}
