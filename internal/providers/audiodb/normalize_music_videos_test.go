package audiodb

import (
	"testing"
	"time"
)

func TestNormalizeArtistMusicVideos(t *testing.T) {
	t.Parallel()
	body := []byte(`{"mvids":[
		{"idTrack":"32793491","strTrack":"High for This","strMusicVid":"http://www.youtube.com/watch?v=JDe86ul6RmI","strDescription":"From Trilogy.","strMusicBrainzArtistID":"` + weekndMBID + `"},
		{"idTrack":"1","strTrack":"Wrong Artist Video","strMusicVid":"https://www.youtube.com/watch?v=x","strMusicBrainzArtistID":"11111111-1111-4111-8111-111111111111"},
		{"idTrack":"2","strTrack":"","strMusicVid":"https://www.youtube.com/watch?v=y"},
		{"idTrack":"3","strTrack":"No URL","strMusicVid":""}
	]}`)
	videos, err := NormalizeArtistMusicVideos(body, weekndMBID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || videos[0].TrackTitle != "High for This" || videos[0].ProviderVideoID != "32793491" {
		t.Fatalf("videos: %+v", videos)
	}
	if videos[0].URL != "http://www.youtube.com/watch?v=JDe86ul6RmI" || videos[0].Description != "From Trilogy." {
		t.Fatalf("video fields: %+v", videos[0])
	}
}

func TestNormalizeArtistMusicVideosEmpty(t *testing.T) {
	t.Parallel()
	videos, err := NormalizeArtistMusicVideos([]byte(`{"mvids":null}`), weekndMBID, time.Now())
	if err != nil || len(videos) != 0 {
		t.Fatalf("expected empty result, got %v %v", videos, err)
	}
}
