package jobs

import "testing"

func TestEpisodicProviderRootIDsMustBePositiveIntegers(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{
		"1931":   true,
		" 1931 ": true,
		"1931-disney-s-adventures-of-the-gummi-bears": false,
		"0": false,
		"":  false,
	} {
		if got := validEpisodicProviderID(value); got != want {
			t.Errorf("validEpisodicProviderID(%q) = %v, want %v", value, got, want)
		}
	}
}
