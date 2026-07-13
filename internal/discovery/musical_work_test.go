package discovery

import "testing"

func TestMusicalWorkComposerOutranksProviderProminence(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindMusicalWork, Query: "Symphony No. 5", Hints: Hints{Composers: []string{"Ludwig van Beethoven"}, Catalogue: "op. 67"}})
	beethoven := Candidate{ProviderScore: 0, Display: Display{Title: "Symphony no. 5 in C minor, op. 67", Catalogue: "op. 67", Artists: []ArtistDisplay{{ID: "145", Name: "Ludwig van Beethoven"}}}}
	other := Candidate{ProviderScore: 100, Display: Display{Title: "Symphony no. 5", Artists: []ArtistDisplay{{ID: "999", Name: "Another Composer"}}}}
	scoreMusicalWorkCandidate(request, &beethoven)
	scoreMusicalWorkCandidate(request, &other)
	if beethoven.Confidence <= other.Confidence || beethoven.Confidence < .7 {
		t.Fatalf("Beethoven=%v other=%v", beethoven.Confidence, other.Confidence)
	}
}

func TestMusicalWorkWithoutIdentityHintsRemainsAmbiguous(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindMusicalWork, Query: "Symphony No. 5"})
	candidate := Candidate{ProviderScore: 100, Display: Display{Title: "Symphony No. 5"}}
	scoreMusicalWorkCandidate(request, &candidate)
	if candidate.Confidence >= .65 {
		t.Fatalf("generic title plus prominence should not auto-resolve: %v", candidate.Confidence)
	}
}
