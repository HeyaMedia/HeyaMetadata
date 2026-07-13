package coverartarchive

import (
	"testing"
	"time"
)

func TestNormalizeReleaseGroupPreservesEveryCoverArtType(t *testing.T) {
	t.Parallel()
	body := []byte(`{"release":"https://musicbrainz.org/release/1","images":[{"id":"123","image":"http://coverartarchive.org/release/1/123.jpg","types":["Front","Booklet","Medium"],"front":true,"back":false,"approved":true},{"id":456,"image":"https://coverartarchive.org/release/1/456.jpg","types":["Back","Spine"],"back":true}]}`)
	record, err := NormalizeReleaseGroup(body, "9162580e-5df4-32de-80cc-f45a8d8a9b1d", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cover", "booklet", "disc", "back_cover", "spine"}
	if len(record.Images) != len(want) {
		t.Fatalf("images: %+v", record.Images)
	}
	for i, class := range want {
		if record.Images[i].Class != class {
			t.Fatalf("image %d class = %q, want %q", i, record.Images[i].Class, class)
		}
	}
	if record.Images[0].SourceURL[:5] != "https" {
		t.Fatalf("source URL was not upgraded: %s", record.Images[0].SourceURL)
	}
}
