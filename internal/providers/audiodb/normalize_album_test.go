package audiodb

import (
	"errors"
	"testing"
	"time"
)

const abbeyRoadRG = "9162580e-5df4-32de-80cc-f45a8d8a9b1d"

const albumBody = `{"album":[{
	"idAlbum":"2109694",
	"strAlbum":"Abbey Road",
	"strMusicBrainzID":"9162580E-5DF4-32DE-80CC-F45A8D8A9B1D",
	"strMusicBrainzArtistID":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d",
	"intYearReleased":"1969",
	"strGenre":"Rock & Roll",
	"strStyle":"Rock/Pop",
	"strMood":"Happy",
	"strLabel":"EMI",
	"intSales":"14300000",
	"intScore":"9.7",
	"intScoreVotes":"6",
	"strDescription":"Abbey Road is the 11th studio album by The Beatles.",
	"strDescriptionFR":"Abbey Road est le onzième album des Beatles.",
	"strReview":"Their last smoke and mirrors masterpiece.",
	"strAlbumThumb":"https://r2.theaudiodb.com/images/thumb.jpg",
	"strAlbumBack":"https://r2.theaudiodb.com/images/back.jpg",
	"strAlbumCDart":"https://r2.theaudiodb.com/images/cdart.png"
}]}`

func TestNormalizeAlbum(t *testing.T) {
	t.Parallel()
	record, err := NormalizeAlbum([]byte(albumBody), abbeyRoadRG, "obs-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "audiodb" || record.ProviderRecord.Namespace != "release_group" || record.ProviderRecord.Value != abbeyRoadRG {
		t.Fatalf("provider record: %+v", record.ProviderRecord)
	}
	if len(record.Titles) != 1 || record.Titles[0].Value != "Abbey Road" {
		t.Fatalf("titles: %+v", record.Titles)
	}
	if len(record.Descriptions) != 2 || record.Descriptions[1].Language != "fr" {
		t.Fatalf("descriptions: %+v", record.Descriptions)
	}
	if len(record.Annotations) != 1 || record.Annotations[0].Type != "provider_review" {
		t.Fatalf("annotations: %+v", record.Annotations)
	}
	if len(record.Genres) != 1 || len(record.Tags) != 2 {
		t.Fatalf("genres/tags: %+v %+v", record.Genres, record.Tags)
	}
	if len(record.Dates) != 1 || record.Dates[0].Value != "1969" {
		t.Fatalf("dates: %+v", record.Dates)
	}
	if len(record.Ratings) != 1 || record.Ratings[0].Value != 9.7 || record.Ratings[0].Votes != 6 || record.Ratings[0].ScaleMax != 10 {
		t.Fatalf("ratings: %+v", record.Ratings)
	}
	if len(record.Metrics) != 1 || record.Metrics[0].Name != "sales" {
		t.Fatalf("metrics: %+v", record.Metrics)
	}
	if len(record.Images) != 3 || record.Images[0].Class != "cover" || record.Images[2].Class != "cdart" {
		t.Fatalf("images: %+v", record.Images)
	}
}

func TestNormalizeAlbumRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeAlbum([]byte(albumBody), "11111111-1111-4111-8111-111111111111", "obs-1", time.Now()); err == nil {
		t.Fatal("expected mismatched release group identity to be rejected")
	}
	if _, err := NormalizeAlbum([]byte(`{"album":null}`), abbeyRoadRG, "obs-1", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty response error = %v, want ErrNotFound", err)
	}
}
