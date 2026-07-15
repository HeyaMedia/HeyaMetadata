package wikidata

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

type monolingualValue struct {
	Language string `json:"language"`
	Value    string `json:"value"`
}

type dataValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type statement struct {
	Rank     string `json:"rank"`
	MainSnak struct {
		SnakType  string    `json:"snaktype"`
		DataValue dataValue `json:"datavalue"`
	} `json:"mainsnak"`
}

func NormalizeArtist(body []byte, expectedID, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var envelope struct {
		Entities map[string]struct {
			ID           string                        `json:"id"`
			Labels       map[string]monolingualValue   `json:"labels"`
			Descriptions map[string]monolingualValue   `json:"descriptions"`
			Aliases      map[string][]monolingualValue `json:"aliases"`
			Claims       map[string][]statement        `json:"claims"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Wikidata artist: %w", err)
	}
	expectedID = strings.ToUpper(strings.TrimSpace(expectedID))
	source, ok := envelope.Entities[expectedID]
	if !ok || source.ID != expectedID || !entityPattern.MatchString(expectedID) {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Wikidata entity %s is missing from response", expectedID)
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord: artistdomain.ProviderRecord{Provider: "wikidata", Namespace: "entity", Value: expectedID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.WikidataNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
	}
	for language, label := range source.Labels {
		if value := strings.TrimSpace(label.Value); value != "" {
			record.Names = append(record.Names, artistdomain.Name{Value: value, Language: language, Type: "label", Primary: language == "en"})
		}
	}
	// Wikidata artist aliases are not scoped tightly enough for canonical artist
	// identity. They frequently contain members, fictional personas, former
	// projects, and other stage-name entities (for example Gorillaz aliases list
	// 2D, Noodle, Murdoc, and Russel). MusicBrainz supplies artist-scoped aliases;
	// keep Wikidata's multilingual labels but do not turn its aliases into names
	// or search keys.
	for language, description := range source.Descriptions {
		if value := strings.TrimSpace(description.Value); value != "" {
			record.Annotations = append(record.Annotations, artistdomain.Text{Value: value, Language: language, Type: "description"})
		}
	}
	// Wikidata may group several distinct MusicBrainz artists and stage-name
	// identities under one real-world person. Its authority identifiers are
	// therefore descriptive evidence only; MusicBrainz supplies the canonical
	// artist crosswalk used by the merge layer.
	musicBrainzArtists := map[string]bool{}
	for _, value := range claimStrings(source.Claims["P434"]) {
		musicBrainzArtists[strings.ToLower(value)] = true
	}
	if len(musicBrainzArtists) > 1 {
		record.Warnings = append(record.Warnings, "wikidata_item_spans_multiple_musicbrainz_artists")
	}
	linkProperties := map[string]string{"P856": "official", "P2397": "youtube", "P2002": "twitter", "P2003": "instagram", "P2013": "facebook", "P7085": "tiktok"}
	for property, kind := range linkProperties {
		for _, value := range claimStrings(source.Claims[property]) {
			if property != "P856" {
				value = socialURL(property, value)
			}
			record.Links = append(record.Links, artistdomain.Link{Type: kind, URL: value})
		}
	}
	for _, value := range claimStrings(source.Claims["P18"]) {
		record.Images = append(record.Images, artistdomain.Image{ProviderImageID: value, SourceURL: "https://commons.wikimedia.org/wiki/Special:Redirect/file/" + url.PathEscape(value), Class: "commons"})
	}
	for _, date := range claimTimes(source.Claims["P571"], "begin") {
		record.Lifecycle.Dates = append(record.Lifecycle.Dates, date)
	}
	for _, date := range claimTimes(source.Claims["P576"], "end") {
		record.Lifecycle.Dates = append(record.Lifecycle.Dates, date)
		ended := true
		record.Lifecycle.Ended = &ended
	}
	for _, property := range []struct{ id, kind string }{{"P740", "formation_location"}, {"P27", "country"}, {"P527", "member"}} {
		for _, entityID := range claimEntityIDs(source.Claims[property.id]) {
			record.Relationships = append(record.Relationships, artistdomain.Relationship{Type: property.kind, TargetProvider: "wikidata", TargetNamespace: "entity", TargetID: entityID})
		}
	}
	if len(record.Names) == 0 {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Wikidata artist %s has no labels", expectedID)
	}
	return record, nil
}

func claimStrings(statements []statement) []string {
	var result []string
	for _, claim := range statements {
		if claim.Rank == "deprecated" || claim.MainSnak.SnakType != "value" || claim.MainSnak.DataValue.Type != "string" {
			continue
		}
		var value string
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &value) == nil && strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func claimEntityIDs(statements []statement) []string {
	var result []string
	for _, claim := range statements {
		if claim.Rank == "deprecated" || claim.MainSnak.SnakType != "value" {
			continue
		}
		var value struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &value) == nil && entityPattern.MatchString(value.ID) {
			result = append(result, value.ID)
		}
	}
	return result
}

func claimTimes(statements []statement, kind string) []artistdomain.DateValue {
	var result []artistdomain.DateValue
	for _, claim := range statements {
		if claim.Rank == "deprecated" || claim.MainSnak.SnakType != "value" || claim.MainSnak.DataValue.Type != "time" {
			continue
		}
		var value struct {
			Time      string `json:"time"`
			Precision int    `json:"precision"`
		}
		if json.Unmarshal(claim.MainSnak.DataValue.Value, &value) != nil {
			continue
		}
		date := strings.TrimPrefix(strings.TrimSuffix(value.Time, "T00:00:00Z"), "+")
		precision := map[int]string{9: "year", 10: "month", 11: "day"}[value.Precision]
		if precision == "year" && len(date) >= 4 {
			date = date[:4]
		} else if precision == "month" && len(date) >= 7 {
			date = date[:7]
		} else if precision == "" {
			precision = "unknown"
		}
		result = append(result, artistdomain.DateValue{Value: date, Precision: precision, Type: kind})
	}
	return result
}

func socialURL(property, value string) string {
	prefix := map[string]string{"P2397": "https://www.youtube.com/channel/", "P2002": "https://twitter.com/", "P2003": "https://www.instagram.com/", "P2013": "https://www.facebook.com/", "P7085": "https://www.tiktok.com/@"}[property]
	return prefix + value
}
