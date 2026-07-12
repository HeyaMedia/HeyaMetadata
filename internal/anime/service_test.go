package anime

import (
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"net/http"
	"testing"
	"time"
)

func TestNormalizePreservesNamedAniDBNumberingSchemes(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`<anime id="23"><type>TV Series</type><episodecount>1</episodecount><startdate>1998-01-01</startdate><titles><title xml:lang="x-jat" type="main">Cowboy Bebop</title><title xml:lang="ja" type="official">カウボーイビバップ</title></titles><tags><tag weight="600"><name>space</name></tag><tag weight="200"><name>noise</name></tag></tags><episodes><episode id="2"><epno type="2">S1</epno><title xml:lang="en">Special</title></episode><episode id="1"><epno type="1">1</epno><title xml:lang="en">Episode One</title></episode></episodes></anime>`)}
	record, err := normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if record.EpisodeCount != 1 || len(record.Episodes) != 2 || record.Episodes[0].Numbers[0].Scheme != "aired" || record.Episodes[1].Numbers[0].Scheme != "special" || record.Episodes[1].Numbers[0].Number != 1 {
		t.Fatalf("episodes: %+v", record.Episodes)
	}
	if len(record.Genres) != 1 || record.Genres[0] != "space" {
		t.Fatalf("genres: %+v", record.Genres)
	}
}
