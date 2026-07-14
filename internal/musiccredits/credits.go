// Package musiccredits handles presentation-only artist credit strings.
//
// A parsed credit is never sufficient evidence to merge canonical artists.
// Callers must first try the complete literal name and prefer structured
// provider artist identifiers whenever they are available.
package musiccredits

import (
	"regexp"
	"strings"
	"unicode"
)

var collaborationSeparator = regexp.MustCompile(`(?i)\s+[\(\[]?(?:feat(?:uring)?\.?|ft\.?|f/|with|w/|vs\.?|versus|pres(?:ents?)?\.?|meets?)\s+|\s+(?:&|and|x|×|\+|/|;|:)\s+|\s*[;；]\s*`)

// SplitFallback parses common collaboration notation after a literal artist
// lookup has failed. It deliberately does not split commas or unspaced slashes
// because both commonly occur inside a single artist's name.
func SplitFallback(value string) []string {
	parts := collaborationSeparator.Split(strings.TrimSpace(value), -1)
	result := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimFunc(strings.TrimSpace(part), func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("()[]{}", r)
		})
		key := strings.ToLower(part)
		if part == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, part)
	}
	return result
}

// ContainsName checks the complete credit before trying presentation syntax.
// equivalent should perform the caller's preferred Unicode/name comparison.
func ContainsName(credit string, names []string, equivalent func(string, string) bool) bool {
	credit = strings.TrimSpace(credit)
	if credit == "" {
		return false
	}
	for _, name := range names {
		if strings.TrimSpace(name) != "" && equivalent(credit, name) {
			return true
		}
	}
	for _, part := range SplitFallback(credit) {
		for _, name := range names {
			if strings.TrimSpace(name) != "" && equivalent(part, name) {
				return true
			}
		}
	}
	return false
}
