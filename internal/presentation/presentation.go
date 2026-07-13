// Package presentation turns canonical multilingual data into a client-ready,
// request-localized view without changing the stored source-of-truth document.
package presentation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
)

type Text struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
	Type     string `json:"type,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
}

type View struct {
	LanguagePreferences []string          `json:"language_preferences"`
	Title               string            `json:"title"`
	TitleLanguage       string            `json:"title_language,omitempty"`
	Description         string            `json:"description,omitempty"`
	DescriptionLanguage string            `json:"description_language,omitempty"`
	Tagline             string            `json:"tagline,omitempty"`
	TaglineLanguage     string            `json:"tagline_language,omitempty"`
	Images              map[string]string `json:"images"`
}

func Apply(document any, kind string, preferences []string, country string, imageSelections map[string]string) (json.RawMessage, error) {
	body, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode canonical document for presentation: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode canonical document for presentation: %w", err)
	}
	display, _ := root["display"].(map[string]any)
	if display == nil {
		display = map[string]any{}
		root["display"] = display
	}
	fallbackTitle := stringValue(display["title"])
	if fallbackTitle == "" {
		fallbackTitle = stringValue(display["name"])
	}

	titles, descriptions, taglines := localizedTextCandidates(root, kind)
	title := Text{Value: fallbackTitle}
	if len(preferences) > 0 {
		if localized := SelectText(titles, preferences, country); localized.Value != "" {
			title = localized
		}
	}
	description := SelectText(descriptions, preferences, country)
	tagline := SelectText(taglines, preferences, country)
	view := View{
		LanguagePreferences: append([]string(nil), preferences...),
		Title:               title.Value, TitleLanguage: title.Language,
		Description: description.Value, DescriptionLanguage: description.Language,
		Tagline: tagline.Value, TaglineLanguage: tagline.Language,
		Images: copySelections(imageSelections),
	}
	root["presentation"] = view
	if title.Value != "" {
		if _, ok := display["name"]; ok && (kind == "artist" || kind == "author") {
			display["name"] = title.Value
		} else {
			display["title"] = title.Value
		}
	}
	for _, class := range []string{"poster", "cover", "profile"} {
		if id := imageSelections[class]; id != "" {
			display["image_id"] = id
			break
		}
	}
	result, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode localized presentation: %w", err)
	}
	return result, nil
}

func SelectText(candidates []Text, preferences []string, country string) Text {
	country = strings.ToUpper(strings.TrimSpace(country))
	type rankedText struct {
		Text
		languageRank int
		countryRank  int
		primaryRank  int
	}
	ranked := make([]rankedText, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.Value = strings.TrimSpace(candidate.Value)
		if candidate.Value == "" {
			continue
		}
		candidate.Language = images.NormalizeLanguage(candidate.Language)
		candidate.Country = strings.ToUpper(strings.TrimSpace(candidate.Country))
		languageRank, _ := images.LanguagePreference(candidate.Language, preferences)
		countryRank := 1
		if country != "" && candidate.Country == country {
			countryRank = 0
		}
		primaryRank := 1
		if candidate.Primary || candidate.Type == "display" || candidate.Type == "main" || candidate.Type == "localized" {
			primaryRank = 0
		}
		ranked = append(ranked, rankedText{Text: candidate, languageRank: languageRank, countryRank: countryRank, primaryRank: primaryRank})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].languageRank != ranked[j].languageRank {
			return ranked[i].languageRank < ranked[j].languageRank
		}
		if ranked[i].countryRank != ranked[j].countryRank {
			return ranked[i].countryRank < ranked[j].countryRank
		}
		if ranked[i].primaryRank != ranked[j].primaryRank {
			return ranked[i].primaryRank < ranked[j].primaryRank
		}
		return ranked[i].Value < ranked[j].Value
	})
	if len(ranked) == 0 {
		return Text{}
	}
	return ranked[0].Text
}

func localizedTextCandidates(root map[string]any, kind string) (titles, descriptions, taglines []Text) {
	data, _ := root["data"].(map[string]any)
	if data == nil {
		return nil, nil, nil
	}
	switch kind {
	case "movie":
		titles = decodeTexts(data["titles"])
		descriptions = decodeTexts(data["overviews"])
		taglines = decodeTexts(data["taglines"])
	case "artist":
		titles = decodeTexts(data["names"])
		descriptions = decodeTexts(data["biographies"])
	case "release_group":
		titles = decodeTexts(data["titles"])
		descriptions = decodeTexts(data["descriptions"])
	case "tv_show", "anime":
		titles = decodeTexts(data["titles"])
		if value := stringValue(data["overview"]); value != "" {
			descriptions = []Text{{Value: value}}
		}
	case "book_work", "book_edition", "manga", "manga_edition", "comic", "comic_edition":
		titles = decodeTexts(data["titles"])
		if value := stringValue(data["description"]); value != "" {
			descriptions = []Text{{Value: value}}
		}
	}
	return titles, descriptions, taglines
}

func decodeTexts(value any) []Text {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var texts []Text
	_ = json.Unmarshal(body, &texts)
	return texts
}

func copySelections(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
