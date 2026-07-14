package canonicalrefs

import "testing"

func TestKeyNormalizesPassiveEvidence(t *testing.T) {
	left := Key(Ref{Provider: " MusicBrainz ", Namespace: "Artist", Value: " E134B52F "})
	right := Key(Ref{Provider: "musicbrainz", Namespace: "artist", Value: "e134b52f"})
	if left != right {
		t.Fatalf("%q != %q", left, right)
	}
}
