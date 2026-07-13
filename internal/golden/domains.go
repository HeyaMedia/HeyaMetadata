package golden

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	semanticcoverage "github.com/HeyaMedia/HeyaMetadata/coverage"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/server"
	"github.com/danielgtaylor/huma/v2"
)

type catalogLoader func() (semanticcoverage.Catalog, error)

func VerifyMovie(ctx context.Context, runtime *platform.Runtime) (Report, error) {
	return verifyCatalog(ctx, runtime, "movie", semanticcoverage.Movie)
}

func VerifyTV(ctx context.Context, runtime *platform.Runtime) (Report, error) {
	return verifyCatalog(ctx, runtime, "tv", semanticcoverage.TV)
}

func VerifyBooks(ctx context.Context, runtime *platform.Runtime) (Report, error) {
	return verifyCatalog(ctx, runtime, "books", semanticcoverage.Books)
}

func VerifyMusic(ctx context.Context, runtime *platform.Runtime) (Report, error) {
	return verifyCatalog(ctx, runtime, "music", semanticcoverage.Music)
}

func verifyCatalog(ctx context.Context, runtime *platform.Runtime, domain string, load catalogLoader) (Report, error) {
	catalog, err := load()
	if err != nil {
		return Report{}, err
	}
	apiServer := server.NewWithRuntime("golden-coverage", runtime)
	testServer := httptest.NewServer(apiServer.Handler())
	defer testServer.Close()

	report := Report{Domain: domain, Checks: []Check{}}
	for _, entry := range catalog.Entries {
		for _, reference := range entry.References {
			check := Check{EntryID: entry.ID, Reference: reference.Name}
			kind := reference.EntityKind
			if kind == "" {
				kind = entry.Kind
			}
			entityID, resolveErr := resolveReference(ctx, runtime, kind, reference.ExternalIDs)
			if resolveErr != nil {
				check.Error = resolveErr.Error()
				report.Checks = append(report.Checks, check)
				continue
			}
			check.EntityID = entityID

			if operationID, capability := capabilityOperation(entry.Projection.Document); capability {
				check.Passed = operationExists(apiServer.API().OpenAPI(), operationID)
				if !check.Passed {
					check.Error = fmt.Sprintf("OpenAPI operation %s is absent", operationID)
				}
			} else {
				check.Passed, check.Error = verifyProjection(ctx, testServer.Client(), testServer.URL, entityID, entry)
			}
			if check.Passed {
				for _, provider := range reference.ExpectedProviders {
					if provider == "system" {
						continue
					}
					present, evidenceErr := providerEvidence(ctx, runtime, entityID, provider)
					if evidenceErr != nil {
						check.Passed, check.Error = false, evidenceErr.Error()
						break
					}
					if !present {
						check.Passed, check.Error = false, fmt.Sprintf("expected %s provenance is absent", provider)
						break
					}
				}
			}
			report.Checks = append(report.Checks, check)
		}
	}
	for _, check := range report.Checks {
		if check.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report, nil
}

func capabilityOperation(document string) (string, bool) {
	operationIDs := map[string]string{
		"resolve":           "resolve-entity",
		"job":               "job-status",
		"search":            "search-entities",
		"change":            "public-changes",
		"discovery":         "create-discovery",
		"fingerprint_match": "match-recording-fingerprint",
	}
	operationID, ok := operationIDs[document]
	return operationID, ok
}

func operationExists(document *huma.OpenAPI, operationID string) bool {
	for _, path := range document.Paths {
		operations := []*huma.Operation{path.Get, path.Put, path.Post, path.Delete, path.Options, path.Head, path.Patch, path.Trace}
		for _, operation := range operations {
			if operation != nil && operation.OperationID == operationID {
				return true
			}
		}
	}
	return false
}

func verifyProjection(ctx context.Context, client *http.Client, baseURL, entityID string, entry semanticcoverage.Entry) (bool, string) {
	path := "/api/v2/entities/" + entityID
	switch entry.Projection.Document {
	case "detail":
	case "credits":
		path += "/credits?limit=250"
		if strings.HasSuffix(entry.ID, ".cast") {
			path += "&credit_type=cast"
		} else if strings.HasSuffix(entry.ID, ".crew") {
			path += "&credit_type=crew"
		}
	case "ratings":
		path += "/ratings?limit=250"
	case "relations":
		path += "/relations?type=" + url.QueryEscape(entry.Projection.Path) + "&limit=100"
	case "fingerprints":
		path = "/api/v2/recordings/" + entityID + "/fingerprints"
	case "lyrics":
		path = "/api/v2/recordings/" + entityID + "/lyrics"
	case "top_tracks":
		path += "/top-tracks?limit=100"
	default:
		return false, fmt.Sprintf("unsupported projection document %s", entry.Projection.Document)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return false, err.Error()
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err.Error()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("%s projection returned HTTP %d", entry.Projection.Document, response.StatusCode)
	}
	var document any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return false, err.Error()
	}
	projectionPath := entry.Projection.Path
	if entry.Projection.Document == "relations" {
		projectionPath = "relations"
	}
	value, ok := valueAtPath(document, projectionPath)
	if !ok || emptyValue(value) {
		return false, fmt.Sprintf("projection path %s is empty", projectionPath)
	}
	return verifyEntrySemantics(entry.ID, value)
}

func verifyEntrySemantics(entryID string, value any) (bool, string) {
	switch entryID {
	case "movie.relationships.cast", "tv.credits.cast":
		if !arrayHasFieldValue(value, "credit_type", "cast") {
			return false, "projection has no cast credits"
		}
	case "movie.relationships.crew", "tv.credits.crew":
		if !arrayHasFieldValue(value, "credit_type", "crew") {
			return false, "projection has no crew credits"
		}
	case "movie.release.certifications":
		if !arrayHasNonEmptyField(value, "certification") {
			return false, "release events have no certification"
		}
	case "movie.numbers.budget_revenue":
		object, ok := value.(map[string]any)
		if !ok || (emptyValue(object["budget"]) && emptyValue(object["revenue"])) {
			return false, "measurements have neither budget nor revenue"
		}
	case "books.edition.identifiers":
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return false, "edition has no external identifiers"
		}
		found := false
		for _, item := range items {
			object, ok := item.(map[string]any)
			if ok && (object["provider"] == "openlibrary" || object["provider"] == "isbn") {
				found = true
				break
			}
		}
		if !found {
			return false, "edition has no Open Library or ISBN identifier"
		}
	case "music.artist.discography.storefront_freshness":
		if !arrayHasRelationTitleAndProvider(value, "Oi AG!", "deezer") {
			return false, "discography has no canonical Oi AG! relation backed by Deezer"
		}
	}
	return true, ""
}

func arrayHasRelationTitleAndProvider(value any, title, provider string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		relation, ok := item.(map[string]any)
		if !ok || emptyValue(relation["target_entity_id"]) {
			continue
		}
		metadata, ok := relation["metadata"].(map[string]any)
		if !ok || !strings.EqualFold(fmt.Sprint(metadata["title"]), title) {
			continue
		}
		sources, ok := metadata["sources"].([]any)
		if !ok {
			continue
		}
		for _, source := range sources {
			object, ok := source.(map[string]any)
			if ok && strings.EqualFold(fmt.Sprint(object["provider"]), provider) {
				return true
			}
		}
	}
	return false
}

func arrayHasFieldValue(value any, field, expected string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && strings.EqualFold(fmt.Sprint(object[field]), expected) {
			return true
		}
	}
	return false
}

func arrayHasNonEmptyField(value any, field string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && !emptyValue(object[field]) {
			return true
		}
	}
	return false
}

func providerEvidence(ctx context.Context, runtime *platform.Runtime, entityID, provider string) (bool, error) {
	var present bool
	err := runtime.DB.QueryRow(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND provider=$2 AND state='accepted')
			OR EXISTS(SELECT 1 FROM normalized_records WHERE entity_id=$1 AND provider=$2)
			OR EXISTS(SELECT 1 FROM image_candidates WHERE entity_id=$1 AND provider=$2)
			OR EXISTS(SELECT 1 FROM entity_relations WHERE (source_entity_id=$1 OR target_entity_id=$1) AND provider=$2 AND state='accepted')
			OR EXISTS(SELECT 1 FROM recording_fingerprints WHERE recording_entity_id=$1 AND source_provider=$2 AND state='ready')
			OR EXISTS(SELECT 1 FROM recording_lyrics WHERE recording_entity_id=$1 AND provider=$2)
			OR EXISTS(
				SELECT 1 FROM canonical_book_editions edition
				JOIN normalized_records record ON record.entity_id=edition.entity_id
				WHERE edition.work_entity_id=$1 AND record.provider=$2
			)
			OR EXISTS(
				SELECT 1 FROM canonical_book_editions edition
				JOIN image_candidates image ON image.entity_id=edition.entity_id
				WHERE edition.work_entity_id=$1 AND image.provider=$2
			)`, entityID, provider).Scan(&present)
	return present, err
}
