package people

import (
	"slices"
	"testing"
)

func TestReconciliationRequiresIndependentEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		evidence   reconciliationEvidence
		automatic  bool
		reasonPart string
	}{
		{name: "name alone", evidence: reconciliationEvidence{mineRootProviders: 1, otherRootProviders: 1}},
		{name: "same-provider namesakes", evidence: reconciliationEvidence{sameBirth: true, canonicalOverlap: 2, providerOverlap: 2, sameRootProvider: true}, reasonPart: "same_provider_roots_require_review"},
		{name: "shared external person ID", evidence: reconciliationEvidence{sharedExternal: true, mineRootProviders: 1, otherRootProviders: 1}, automatic: true},
		{name: "birth and filmography", evidence: reconciliationEvidence{sameBirth: true, canonicalOverlap: 1, providerOverlap: 1, mineRootProviders: 1, otherRootProviders: 1}, automatic: true},
		{name: "multiple filmography matches", evidence: reconciliationEvidence{canonicalOverlap: 1, providerOverlap: 2, mineRootProviders: 1, otherRootProviders: 1}, automatic: true},
		{name: "established provider consensus", evidence: reconciliationEvidence{canonicalOverlap: 1, providerOverlap: 1, mineRootProviders: 2, otherRootProviders: 1}, automatic: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, reasons, automaticReason := assessReconciliation(test.evidence)
			if (automaticReason != "") != test.automatic {
				t.Fatalf("automatic reason = %q", automaticReason)
			}
			if test.reasonPart != "" && !slices.Contains(reasons, test.reasonPart) {
				t.Fatalf("reasons = %#v", reasons)
			}
		})
	}
}
