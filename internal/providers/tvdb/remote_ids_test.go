package tvdb

import "testing"

func TestNormalizeTMDBIDHandlesTVDBSlugValues(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"1931": "1931",
		"1931-disney-s-adventures-of-the-gummi-bears": "1931",
		" 00603-the-matrix ":                          "603",
		"disney-s-adventures":                         "",
		"0-invalid":                                   "",
		"":                                            "",
	} {
		if got := NormalizeTMDBID(input); got != want {
			t.Errorf("NormalizeTMDBID(%q) = %q, want %q", input, got, want)
		}
	}
}
