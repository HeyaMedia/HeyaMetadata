package people

import "testing"

func TestEquivalentCreditCollapsesCrossProviderSelfRoles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		left  CanonicalCredit
		right CanonicalCredit
	}{
		{
			name:  "host name and self host",
			left:  CanonicalCredit{PersonEntityID: "person", Provider: "tmdb", DisplayName: "Jeremy Clarkson", CreditType: "cast", Character: "Self - Host"},
			right: CanonicalCredit{PersonEntityID: "person", Provider: "tvmaze", DisplayName: "Jeremy Clarkson", CreditType: "cast", Character: "Jeremy Clarkson"},
		},
		{
			name:  "driver and self driver",
			left:  CanonicalCredit{PersonEntityID: "person", Provider: "tmdb", DisplayName: "Abbie Eaton", CreditType: "cast", Character: "Self - Driver"},
			right: CanonicalCredit{PersonEntityID: "person", Provider: "tvmaze", DisplayName: "Abbie Eaton", CreditType: "cast", Character: "Driver"},
		},
		{
			name:  "missing and named self role",
			left:  CanonicalCredit{PersonEntityID: "person", Provider: "tvmaze", DisplayName: "Francis Bourgeois", CreditType: "cast", Character: "Francis Bourgeois"},
			right: CanonicalCredit{PersonEntityID: "person", Provider: "tvdb", DisplayName: "Francis Bourgeois", CreditType: "cast"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !equivalentCredit(test.left, test.right, 1) {
				t.Fatal("expected equivalent cross-provider credit")
			}
		})
	}
}

func TestEquivalentCreditPreservesDistinctIdentitiesAndRoles(t *testing.T) {
	t.Parallel()
	base := CanonicalCredit{PersonEntityID: "person-a", Provider: "tmdb", ProviderPersonID: "1958624", DisplayName: "Ben Joiner", CreditType: "crew", Department: "Camera", Job: "Director of Photography"}
	namesake := base
	namesake.PersonEntityID = "person-b"
	namesake.ProviderPersonID = "1423708"
	if equivalentCredit(base, namesake, 0) {
		t.Fatal("same-name people with different canonical IDs were collapsed")
	}
	differentJob := base
	differentJob.Provider = "tvdb"
	differentJob.Job = "Camera Operator"
	if equivalentCredit(base, differentJob, 0) {
		t.Fatal("genuinely different crew jobs were collapsed")
	}
	differentCharacter := CanonicalCredit{PersonEntityID: "actor", Provider: "tmdb", CreditType: "cast", Character: "Character A"}
	otherCharacter := differentCharacter
	otherCharacter.Provider = "tvmaze"
	otherCharacter.Character = "Character B"
	if equivalentCredit(differentCharacter, otherCharacter, 2) {
		t.Fatal("genuinely different cast roles were collapsed")
	}
	genericSelf := differentCharacter
	genericSelf.Provider = "tvdb"
	genericSelf.Character = "Self"
	if equivalentCredit(genericSelf, differentCharacter, 2) {
		t.Fatal("generic self credit erased one of multiple distinct cast roles")
	}
}
