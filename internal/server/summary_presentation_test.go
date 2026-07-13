package server

import (
	"encoding/json"
	"testing"

	presentationview "github.com/HeyaMedia/HeyaMetadata/internal/presentation"
)

func TestLocalizeSummaryUsesRequestedArtistAliasWithoutMutatingCanonicalSummary(t *testing.T) {
	t.Parallel()
	canonical := json.RawMessage(`{"id":"artist-id","kind":"artist","display":{"name":"新しい学校のリーダーズ"}}`)
	names := map[string][]presentationview.Text{
		"artist-id": {
			{Value: "新しい学校のリーダーズ", Language: "ja", Type: "label", Quality: 80},
			{Value: "ATARASHII GAKKO!", Language: "en", Type: "artist_name", Quality: 100},
		},
	}

	localized, err := localizeSummary(canonical, names, []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Display struct {
			Name string `json:"name"`
		} `json:"display"`
	}
	if err := json.Unmarshal(localized, &result); err != nil {
		t.Fatal(err)
	}
	if result.Display.Name != "ATARASHII GAKKO!" {
		t.Fatalf("localized name = %q", result.Display.Name)
	}
	if string(canonical) != `{"id":"artist-id","kind":"artist","display":{"name":"新しい学校のリーダーズ"}}` {
		t.Fatalf("canonical summary was mutated: %s", canonical)
	}
}

func TestLocalizeSummaryUsesRequestedJapaneseName(t *testing.T) {
	t.Parallel()
	canonical := json.RawMessage(`{"id":"artist-id","kind":"artist","display":{"name":"ATARASHII GAKKO!"}}`)
	names := map[string][]presentationview.Text{
		"artist-id": {
			{Value: "ATARASHII GAKKO!", Language: "ja", Type: "alias", Quality: 50},
			{Value: "新しい学校のリーダーズ", Language: "ja", Type: "label", Quality: 80},
		},
	}

	localized, err := localizeSummary(canonical, names, []string{"ja"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Display struct {
			Name string `json:"name"`
		} `json:"display"`
	}
	if err := json.Unmarshal(localized, &result); err != nil {
		t.Fatal(err)
	}
	if result.Display.Name != "新しい学校のリーダーズ" {
		t.Fatalf("localized name = %q", result.Display.Name)
	}
}
