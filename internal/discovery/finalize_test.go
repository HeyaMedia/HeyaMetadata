package discovery

import "testing"

func TestFinalizeSearchResultReturnsDecisiveExistingEntity(t *testing.T) {
	result := Result{
		Status:         "completed",
		Recommendation: "likely_match",
		Candidates: []Candidate{{
			Confidence:       .789,
			ExistingEntityID: "a6e0479b-86e0-4931-93d3-3b910ba850e9",
			Resolution:       Resolution{Kind: KindTVShow, Provider: "tvmaze", Namespace: "show", Value: "526"},
		}},
	}
	FinalizeSearchResult(&result)
	if result.Status != "completed" || result.Recommendation != "existing_entity" || result.EntityID != "a6e0479b-86e0-4931-93d3-3b910ba850e9" || len(result.Candidates) != 0 {
		t.Fatalf("result: %+v", result)
	}
}

func TestFinalizeSearchResultKeepsAmbiguousProductionsReviewable(t *testing.T) {
	result := Result{
		Status:         "completed",
		Recommendation: "ambiguous",
		Candidates: []Candidate{
			{Confidence: .79, ExistingEntityID: "existing"},
			{Confidence: .76, ExistingEntityID: "different"},
		},
	}
	FinalizeSearchResult(&result)
	if result.Status != "needs_selection" || result.EntityID != "" || len(result.Candidates) != 2 {
		t.Fatalf("result: %+v", result)
	}
}

func TestFinalizeSearchResultDoesNotTrustLoneWeakKnownHit(t *testing.T) {
	result := Result{
		Status:         "completed",
		Recommendation: "ambiguous",
		Candidates:     []Candidate{{Confidence: .31, ExistingEntityID: "existing"}},
	}
	FinalizeSearchResult(&result)
	if result.Status != "needs_selection" || result.EntityID != "" {
		t.Fatalf("result: %+v", result)
	}
}
