package discogs

import (
	"testing"
	"time"
)

func TestNormalizeReleasePreservesBarcodeAndTrackLayout(t *testing.T) {
	body := []byte(`{"id":12,"title":"Zanmu","country":"Japan","released":"2024-07-10","uri":"https://discogs.com/release/12","identifiers":[{"type":"Barcode","value":"602468033400"}],"artists":[{"id":7,"name":"Ado"}],"tracklist":[{"position":"1","type_":"track","title":"Show","duration":"3:20"}]}`)
	r, err := NormalizeRelease(body, "obs", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.Editions[0].Barcode != "602468033400" || len(r.Tracks) != 1 || r.Tracks[0].Position != "1" {
		t.Fatalf("record: %+v", r)
	}
}
