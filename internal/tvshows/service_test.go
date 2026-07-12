package tvshows

import (
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"net/http"
	"testing"
	"time"
)

func TestNormalizeTVMazeEmbedsSeasonsEpisodesAndExternalIDs(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`{"id":82,"name":"Game of Thrones","type":"Scripted","language":"English","status":"Ended","premiered":"2011-04-17","genres":["Drama"],"network":{"name":"HBO","country":{"code":"US"}},"externals":{"thetvdb":121361,"imdb":"tt0944947"},"_embedded":{"seasons":[{"id":1,"number":1}],"episodes":[{"id":2,"name":"Winter Is Coming","season":1,"number":1,"airdate":"2011-04-17"}]}}`)}
	record, err := normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if record.Language != "en" || len(record.Countries) != 1 || record.Countries[0] != "US" || len(record.Seasons) != 1 || len(record.Episodes) != 1 || len(record.ExternalIDs) != 3 {
		t.Fatalf("record: %+v", record)
	}
}
