package discovery

import "testing"

func TestRecordingHintsRankExactISRCAndDuration(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindRecording, Query: "Come Together", Hints: Hints{Artists: []string{"The Beatles"}, DurationMS: 259000, ISRCs: []string{"gbaye0601690"}, Releases: []ReleaseHint{{Title: "Abbey Road", Year: 1969}}}})
	exact := Candidate{ProviderScore: 90, Display: Display{Title: "Come Together", DurationMS: 260000, ISRCs: []string{"GBAYE0601690"}, Artists: []ArtistDisplay{{Name: "The Beatles"}}, Releases: []ReleaseHint{{Title: "Abbey Road", Year: 1969}}}}
	wrong := Candidate{ProviderScore: 100, Display: Display{Title: "Come Together", DurationMS: 300000, ISRCs: []string{"OTHER"}, Artists: []ArtistDisplay{{Name: "Cover Band"}}}}
	scoreRecordingCandidate(request, &exact)
	scoreRecordingCandidate(request, &wrong)
	if exact.Confidence <= wrong.Confidence || exact.Confidence < .9 {
		t.Fatalf("exact=%v wrong=%v", exact.Confidence, wrong.Confidence)
	}
}
