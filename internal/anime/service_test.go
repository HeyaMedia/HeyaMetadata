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

func TestNormalizeDoesNotClaimAmbiguousRelatedExternalIDs(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`<anime id="23"><titles><title xml:lang="en" type="main">Cowboy Bebop</title></titles><resources><resource type="2"><externalentity><identifier>1</identifier></externalentity><externalentity><identifier>4037</identifier></externalentity></resource><resource type="44"><externalentity><identifier>30991</identifier><identifier>tv</identifier></externalentity></resource></resources></anime>`)}
	record, err := normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range record.ExternalIDs {
		if id.Provider == "myanimelist" {
			t.Fatalf("ambiguous MAL claim escaped: %+v", id)
		}
	}
	foundTMDB := false
	for _, id := range record.ExternalIDs {
		if id.Provider == "tmdb" && id.Namespace == "tv" && id.Value == "30991" {
			foundTMDB = true
		}
	}
	if !foundTMDB {
		t.Fatalf("unambiguous TMDB ID missing: %+v", record.ExternalIDs)
	}
}

func TestTVDBAnimeMappingPreservesTVDBAndRelativeAniDBNumbers(t *testing.T) {
	payload:=providers.Payload{ObservationID:"obs",ObservedAt:time.Unix(1,0),Body:[]byte(`{"data":{"id":7,"name":"Split Cour","episodes":[{"id":9,"name":"Return","seasonNumber":1,"number":13}]}}`)}
	record,err:=normalizeTVDBAnime(payload,1,12);if err!=nil{t.Fatal(err)}
	if len(record.Episodes)!=1||len(record.Episodes[0].Numbers)!=2||record.Episodes[0].Numbers[0].Number!=13||record.Episodes[0].Numbers[1].Number!=1{t.Fatalf("numbers: %+v",record.Episodes)}
}
