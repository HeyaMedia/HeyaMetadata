package discovery

import "testing"

func TestTVScoringUsesClientAliasEvidence(t *testing.T) {
	candidate := Candidate{ProviderScore: 100, Identity: ExternalID{Provider: "tmdb", Namespace: "tv", Value: "2316"}, Display: Display{Name: "The Office", Year: 2005}}
	scoreTVCandidate(Request{Kind: KindTVShow, Query: "The Office (US)", Hints: Hints{Aliases: []string{"The Office"}, Year: 2005}}, &candidate)
	if candidate.Confidence < .65 || candidate.Match != "likely" {
		t.Fatalf("candidate=%+v", candidate)
	}
	foundAliasEvidence := false
	for _, evidence := range candidate.Evidence {
		if evidence.Field == "title" && evidence.Outcome == "exact_hint_alias" {
			foundAliasEvidence = true
		}
	}
	if !foundAliasEvidence {
		t.Fatalf("evidence=%+v", candidate.Evidence)
	}
}
