package discogs

import (
	"testing"
	"time"
)

func TestNormalizeReleasePreservesBarcodeAndTrackLayout(t *testing.T) {
	body := []byte(`{"id":12,"title":"Zanmu","country":"Japan","released":"2024-07-10","uri":"https://discogs.com/release/12","identifiers":[{"type":"Barcode","value":"602468033400"}],"artists":[{"id":7,"name":"Ado"}],"labels":[{"id":333,"name":"Virgin Music","catno":"UPCH-20637"}],"formats":[{"name":"CD","qty":"1"},{"name":"","qty":"0"}],"tracklist":[{"position":"1","type_":"track","title":"Show","duration":"3:20"}]}`)
	r, err := NormalizeRelease(body, "obs", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.Editions[0].Barcode != "602468033400" || len(r.Tracks) != 1 || r.Tracks[0].Position != "1" {
		t.Fatalf("record: %+v", r)
	}
	if len(r.Editions[0].Labels) != 1 || r.Editions[0].Labels[0].Name != "Virgin Music" || r.Editions[0].Labels[0].CatalogNumber != "UPCH-20637" || r.Editions[0].Labels[0].ProviderID != "333" {
		t.Fatalf("labels: %+v", r.Editions[0].Labels)
	}
	if len(r.Editions[0].Formats) != 1 || r.Editions[0].Formats[0] != "CD" {
		t.Fatalf("formats: %+v", r.Editions[0].Formats)
	}
}
