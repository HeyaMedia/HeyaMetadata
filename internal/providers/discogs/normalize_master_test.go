package discogs

import (
	"testing"
	"time"
)

func TestNormalizeMasterPreservesRepresentativeTracklistAndEdition(t *testing.T) {
	body := []byte(`{"id":24047,"title":"Abbey Road","year":1969,"main_release":2607424,"uri":"https://www.discogs.com/master/24047","genres":["Rock"],"styles":["Pop Rock"],"num_for_sale":5904,"lowest_price":0.7,"data_quality":"Correct","artists":[{"id":82730,"name":"The Beatles","anv":"","join":""}],"images":[{"type":"primary","resource_url":"https://i.discogs.com/cover.jpeg","width":600,"height":600}],"tracklist":[{"position":"A1","type_":"track","title":"Come Together","duration":"4:20"},{"position":"B1","type_":"track","title":"Here Comes the Sun","duration":"3:05"}]}`)
	record, err := NormalizeMaster(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Value != "24047" || record.ArtistCredits[0].ArtistID != "82730" || record.Editions[0].ProviderID != "2607424" {
		t.Fatalf("record: %+v", record)
	}
	if len(record.Tracks) != 2 || record.Tracks[0].DurationMS != 260000 || record.Images[0].Class != "cover" {
		t.Fatalf("tracks/images: %+v / %+v", record.Tracks, record.Images)
	}
}
