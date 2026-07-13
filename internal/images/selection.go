package images

import (
	"sort"
	"strings"

	xlang "golang.org/x/text/language"
)

type EntityImageCandidate struct {
	ID                   string  `json:"id"`
	Class                string  `json:"class"`
	Language             string  `json:"language,omitempty"`
	Country              string  `json:"country,omitempty"`
	Width                int     `json:"width,omitempty"`
	Height               int     `json:"height,omitempty"`
	Provider             string  `json:"provider"`
	ProviderScore        float64 `json:"provider_score,omitempty"`
	MaterializationState string  `json:"materialization_state"`
	Selected             bool    `json:"selected"`
	SelectionReason      string  `json:"selection_reason"`
	languageRank         int
	countryRank          int
}

func LanguagePreferences(explicit, fallbacks, acceptLanguage string) []string {
	preferences := make([]string, 0, 8)
	appendValues := func(raw string) {
		for _, value := range strings.Split(raw, ",") {
			if normalized := NormalizeLanguage(strings.TrimSpace(strings.SplitN(value, ";", 2)[0])); normalized != "" {
				preferences = appendUnique(preferences, normalized)
			}
		}
	}
	appendValues(explicit)
	appendValues(fallbacks)
	if strings.TrimSpace(acceptLanguage) != "" {
		if tags, quality, err := xlang.ParseAcceptLanguage(acceptLanguage); err == nil {
			for index, tag := range tags {
				if index < len(quality) && quality[index] <= 0 {
					continue
				}
				if normalized := NormalizeLanguage(tag.String()); normalized != "" {
					preferences = appendUnique(preferences, normalized)
				}
			}
		}
	}
	return preferences
}

func NormalizeLanguage(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" || value == "00" || strings.EqualFold(value, "null") || strings.EqualFold(value, "und") || value == "*" {
		return ""
	}
	tag, err := xlang.Parse(value)
	if err != nil {
		return strings.ToLower(value)
	}
	return tag.String()
}

func RankCandidates(candidates []EntityImageCandidate, preferences []string, country string) []EntityImageCandidate {
	country = strings.ToUpper(strings.TrimSpace(country))
	for index := range candidates {
		candidate := &candidates[index]
		candidate.Language = NormalizeLanguage(candidate.Language)
		candidate.Country = strings.ToUpper(strings.TrimSpace(candidate.Country))
		candidate.languageRank, candidate.SelectionReason = LanguagePreference(candidate.Language, preferences)
		candidate.countryRank = 1
		if country != "" && candidate.Country == country {
			candidate.countryRank = 0
		}
		candidate.Selected = false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if a.languageRank != b.languageRank {
			return a.languageRank < b.languageRank
		}
		if a.countryRank != b.countryRank {
			return a.countryRank < b.countryRank
		}
		if a.ProviderScore != b.ProviderScore {
			return a.ProviderScore > b.ProviderScore
		}
		areaA, areaB := int64(a.Width)*int64(a.Height), int64(b.Width)*int64(b.Height)
		if areaA != areaB {
			return areaA > areaB
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.ID < b.ID
	})
	seenClass := map[string]bool{}
	for index := range candidates {
		if !seenClass[candidates[index].Class] {
			candidates[index].Selected = true
			seenClass[candidates[index].Class] = true
		}
	}
	return candidates
}

func LanguagePreference(candidate string, preferences []string) (int, string) {
	if len(preferences) == 0 {
		if candidate == "" {
			return 0, "neutral"
		}
		return 1000, "fallback"
	}
	for index, preferred := range preferences {
		if candidate == preferred {
			return index * 10, "exact_language"
		}
		if candidate != "" && baseLanguage(candidate) == baseLanguage(preferred) && scriptsCompatible(candidate, preferred) {
			return index*10 + 1, "base_language"
		}
	}
	if candidate == "" {
		return 500, "neutral"
	}
	return 1000, "fallback"
}

func scriptsCompatible(left, right string) bool {
	leftScript, rightScript := explicitScript(left), explicitScript(right)
	return leftScript == "" || rightScript == "" || leftScript == rightScript
}

func explicitScript(value string) string {
	parts := strings.Split(value, "-")
	for _, part := range parts[1:] {
		if len(part) == 4 {
			return strings.ToLower(part)
		}
	}
	return ""
}

func baseLanguage(value string) string {
	tag, err := xlang.Parse(value)
	if err != nil {
		return strings.ToLower(strings.SplitN(value, "-", 2)[0])
	}
	base, _ := tag.Base()
	return base.String()
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
