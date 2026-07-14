package discovery

// FinalizeSearchResult turns a ranked upstream search into its public control
// flow. A provider identity that is already accepted on a canonical entity can
// complete directly, but only when the normal evidence ranking says the match
// is decisive. Title/year similarity alone never creates or merges identity.
func FinalizeSearchResult(result *Result) {
	if result == nil {
		return
	}
	if entityID := decisiveExistingEntity(*result); entityID != "" {
		result.Status = "completed"
		result.Recommendation = "existing_entity"
		result.EntityID = entityID
		result.Candidates = nil
		return
	}
	result.EntityID = ""
	result.Status = "completed"
	if len(result.Candidates) > 0 {
		result.Status = "needs_selection"
	}
}

func decisiveExistingEntity(result Result) string {
	if len(result.Candidates) == 0 {
		return ""
	}
	top := result.Candidates[0]
	if top.ExistingEntityID == "" {
		return ""
	}
	if result.Recommendation == "strong_match" || result.Recommendation == "likely_match" {
		return top.ExistingEntityID
	}
	// Multiple provider hits can occasionally be aliases of one accepted Heya
	// identity. They are safe to collapse only when every hit converges and the
	// top candidate is itself at least likely; a lone weak hit stays reviewable.
	if len(result.Candidates) < 2 || top.Confidence < .65 {
		return ""
	}
	for _, candidate := range result.Candidates[1:] {
		if candidate.ExistingEntityID != top.ExistingEntityID {
			return ""
		}
	}
	return top.ExistingEntityID
}
