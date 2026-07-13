package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	presentationview "github.com/HeyaMedia/HeyaMetadata/internal/presentation"
)

// localizeSummaries applies request-local name/title selection to compact
// search projections. Canonical documents and their stored search summaries
// remain unchanged; only this response copy is rewritten.
func localizeSummaries(ctx context.Context, runtime *platform.Runtime, summaries []json.RawMessage, preferences []string) ([]json.RawMessage, error) {
	if len(summaries) == 0 || len(preferences) == 0 {
		return summaries, nil
	}

	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		var envelope struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(summary, &envelope); err != nil {
			return nil, fmt.Errorf("decode compact entity for presentation: %w", err)
		}
		if strings.TrimSpace(envelope.ID) != "" {
			ids = append(ids, envelope.ID)
		}
	}
	if len(ids) == 0 {
		return summaries, nil
	}

	rows, err := runtime.DB.Query(ctx, `
		SELECT entity_id::text,value,locale,name_type,source_quality
		FROM search_names
		WHERE entity_id=ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[string][]presentationview.Text, len(ids))
	for rows.Next() {
		var entityID string
		var candidate presentationview.Text
		if err := rows.Scan(&entityID, &candidate.Value, &candidate.Language, &candidate.Type, &candidate.Quality); err != nil {
			return nil, err
		}
		names[entityID] = append(names[entityID], candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	localized := make([]json.RawMessage, 0, len(summaries))
	for _, summary := range summaries {
		body, err := localizeSummary(summary, names, preferences)
		if err != nil {
			return nil, err
		}
		localized = append(localized, body)
	}
	return localized, nil
}

func localizeSummary(summary json.RawMessage, names map[string][]presentationview.Text, preferences []string) (json.RawMessage, error) {
	var root map[string]any
	if err := json.Unmarshal(summary, &root); err != nil {
		return nil, fmt.Errorf("decode compact entity for presentation: %w", err)
	}
	entityID, _ := root["id"].(string)
	selected := presentationview.SelectText(names[entityID], preferences, "")
	if selected.Value == "" {
		return append(json.RawMessage(nil), summary...), nil
	}
	display, _ := root["display"].(map[string]any)
	if display == nil {
		display = map[string]any{}
		root["display"] = display
	}
	kind, _ := root["kind"].(string)
	if compactKindUsesName(kind) {
		display["name"] = selected.Value
	} else {
		display["title"] = selected.Value
	}
	body, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode localized compact entity: %w", err)
	}
	return body, nil
}

func compactKindUsesName(kind string) bool {
	switch kind {
	case "artist", "author", "person":
		return true
	default:
		return false
	}
}
