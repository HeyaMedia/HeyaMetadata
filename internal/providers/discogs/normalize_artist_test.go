package discogs

import (
	"testing"
	"time"
)

func TestNormalizeArtistKeepsAliasesAsRelationships(t *testing.T) {
	record, err := NormalizeArtist([]byte(`{"id":82730,"name":"The Beatles","realname":"The Beatles","profile":"English rock band [a46481]","namevariations":["Beatles"],"aliases":[{"id":99,"name":"Not an identity alias"}],"members":[{"id":46481,"name":"John Lennon","active":false}],"images":[{"type":"primary","resource_url":"https://img/full.jpg","width":600,"height":600}],"urls":["https://thebeatles.com"],"data_quality":"Needs Vote"}`), "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.IdentityCandidates) != 1 {
		t.Fatalf("aliases must not become identities: %+v", record.IdentityCandidates)
	}
	if len(record.Relationships) != 2 || record.Relationships[0].Type != "discogs_alias" || record.Relationships[1].Ended == nil || !*record.Relationships[1].Ended {
		t.Fatalf("relationships: %+v", record.Relationships)
	}
	if record.Biographies[0].Markup != "discogs" || record.Images[0].Width != 600 {
		t.Fatalf("record: %+v", record)
	}
}
