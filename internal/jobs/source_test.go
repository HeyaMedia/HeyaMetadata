package jobs

import "testing"

func TestSourceCollectionUniquenessExcludesCredentials(t *testing.T) {
	t.Parallel()
	args := SourceCollectArgs{Provider: "musicbrainz", IdentifierProvider: "musicbrainz", Namespace: "artist", Value: "id", CredentialRef: "secret-ref"}
	opts := args.InsertOpts()
	if !opts.UniqueOpts.ByArgs || opts.Priority != PriorityInteractive {
		t.Fatalf("source collection options: %+v", opts)
	}
}
