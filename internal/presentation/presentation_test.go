package presentation

import (
	"encoding/json"
	"testing"
)

func TestApplyLocalizesTitleTextAndPrimaryImage(t *testing.T) {
	t.Parallel()
	document := map[string]any{
		"display": map[string]any{"title": "Original", "image_id": "wrong-language"},
		"data": map[string]any{
			"titles":    []map[string]any{{"value": "Русский", "language": "ru"}, {"value": "English", "language": "eng"}},
			"overviews": []map[string]any{{"value": "Neutral overview"}, {"value": "English overview", "language": "en"}},
		},
	}
	body, err := Apply(document, "movie", []string{"en-GB"}, "GB", map[string]string{"poster": "english-poster", "backdrop": "neutral-backdrop"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Display struct {
			Title   string `json:"title"`
			ImageID string `json:"image_id"`
		}
		Presentation View `json:"presentation"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Display.Title != "English" || result.Display.ImageID != "english-poster" {
		t.Fatalf("display: %+v", result.Display)
	}
	if result.Presentation.Description != "English overview" || result.Presentation.Images["backdrop"] != "neutral-backdrop" {
		t.Fatalf("presentation: %+v", result.Presentation)
	}
}

func TestSelectTextPrefersNeutralBeforeWrongLanguage(t *testing.T) {
	t.Parallel()
	selected := SelectText([]Text{{Value: "Русский", Language: "ru"}, {Value: "Neutral"}}, []string{"en"}, "")
	if selected.Value != "Neutral" {
		t.Fatalf("selected %+v", selected)
	}
}

func TestSelectTextUsesSourceQualityWithinRequestedLanguage(t *testing.T) {
	t.Parallel()
	selected := SelectText([]Text{
		{Value: "ATARASHII GAKKO!", Language: "ja", Type: "alias", Quality: 50},
		{Value: "新しい学校のリーダーズ", Language: "ja", Type: "label", Quality: 80},
	}, []string{"ja"}, "")
	if selected.Value != "新しい学校のリーダーズ" {
		t.Fatalf("selected %+v", selected)
	}
}
