package jobs

import (
	"reflect"
	"testing"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func TestMusicBrainzReleaseIDsSelectsIssuedEditions(t *testing.T) {
	t.Parallel()
	editions := []rgdomain.ProjectedEdition{
		{Provider: "musicbrainz", Namespace: "release", ProviderID: "first"},
		{Provider: "discogs", Namespace: "release", ProviderID: "discogs"},
		{Provider: "musicbrainz", Namespace: "release", ProviderID: "first"},
		{Provider: "musicbrainz", Namespace: "release", ProviderID: "second"},
	}
	if got, want := musicBrainzReleaseIDs(editions), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("release IDs: got %v, want %v", got, want)
	}
}
