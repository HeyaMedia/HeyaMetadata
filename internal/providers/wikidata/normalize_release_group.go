package wikidata

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func NormalizeReleaseGroup(body []byte, expectedID, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
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
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Wikidata release group: %w", err)
	}
	expectedID = strings.ToUpper(strings.TrimSpace(expectedID))
	source, ok := envelope.Entities[expectedID]
	if !ok || source.ID != expectedID {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Wikidata entity %s is missing from response", expectedID)
	}
	record := rgdomain.NormalizedRecordV1{ProviderRecord: rgdomain.ProviderRecord{Provider: "wikidata", Namespace: "entity", Value: expectedID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.WikidataNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion}, IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "wikidata", Namespace: "entity", NormalizedValue: expectedID, Confidence: 1, Evidence: "provider_record"}}}
	for language, label := range source.Labels {
		if value := strings.TrimSpace(label.Value); value != "" {
			record.Titles = append(record.Titles, rgdomain.Title{Value: value, Language: language, Type: "label", Primary: language == "en"})
		}
	}
	for language, aliases := range source.Aliases {
		for _, alias := range aliases {
			if value := strings.TrimSpace(alias.Value); value != "" {
				record.Titles = append(record.Titles, rgdomain.Title{Value: value, Language: language, Type: "alias"})
			}
		}
	}
	for language, description := range source.Descriptions {
		if value := strings.TrimSpace(description.Value); value != "" {
			record.Descriptions = append(record.Descriptions, rgdomain.Text{Value: value, Language: language, Type: "description"})
		}
	}
	identities := map[string]struct{ provider, namespace string }{"P436": {"musicbrainz", "release_group"}, "P1954": {"discogs", "master"}, "P2205": {"spotify", "album"}, "P2281": {"apple", "album"}, "P2723": {"deezer", "album"}, "P1729": {"allmusic", "album"}}
	for property, target := range identities {
		for _, value := range claimStrings(source.Claims[property]) {
			record.IdentityCandidates = append(record.IdentityCandidates, rgdomain.IdentityCandidate{Provider: target.provider, Namespace: target.namespace, NormalizedValue: value, Confidence: 1, Evidence: "wikidata_" + property})
		}
	}
	for _, value := range claimStrings(source.Claims["P18"]) {
		record.Images = append(record.Images, rgdomain.Image{ProviderImageID: value, SourceURL: "https://commons.wikimedia.org/wiki/Special:Redirect/file/" + url.PathEscape(value), Class: "cover"})
	}
	record.Dates = append(record.Dates, releaseGroupClaimTimes(source.Claims["P577"], "publication")...)
	for i, entityID := range claimEntityIDs(source.Claims["P175"]) {
		record.ArtistCredits = append(record.ArtistCredits, rgdomain.ArtistCredit{Position: i, Name: entityID, ArtistProvider: "wikidata", ArtistNamespace: "entity", ArtistID: entityID})
	}
	for _, value := range claimStrings(source.Claims["P4300"]) {
		record.Links = append(record.Links, rgdomain.Link{Type: "youtube_music_playlist", URL: "https://music.youtube.com/playlist?list=" + url.QueryEscape(value)})
	}
	if len(record.Titles) == 0 {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Wikidata release group %s has no labels", expectedID)
	}
	return record, nil
}

func releaseGroupClaimTimes(statements []statement, kind string) []rgdomain.DateValue {
	var result []rgdomain.DateValue
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
		result = append(result, rgdomain.DateValue{Value: date, Precision: precision, Type: kind})
	}
	return result
}
