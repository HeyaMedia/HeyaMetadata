package acceptance_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// publicOperations is intentionally explicit. Adding, removing, or renaming a
// public operation is a contract decision and must update this acceptance
// catalog alongside the generated SDK.
var publicOperations = map[string]struct {
	method string
	path   string
}{
	"connectivity-ip":             {http.MethodGet, "/v1/ip"},
	"connectivity-check":          {http.MethodPost, "/v1/check"},
	"health-live":                 {http.MethodGet, "/api/v2/health/live"},
	"health-ready":                {http.MethodGet, "/api/v2/health/ready"},
	"auth-register":               {http.MethodPost, "/api/v2/auth/register"},
	"auth-challenge":              {http.MethodGet, "/api/v2/auth/challenge"},
	"auth-login":                  {http.MethodPost, "/api/v2/auth/login"},
	"auth-logout":                 {http.MethodPost, "/api/v2/auth/logout"},
	"auth-me":                     {http.MethodGet, "/api/v2/auth/me"},
	"create-api-key":              {http.MethodPost, "/api/v2/auth/api-keys"},
	"list-api-keys":               {http.MethodGet, "/api/v2/auth/api-keys"},
	"revoke-api-key":              {http.MethodDelete, "/api/v2/auth/api-keys/{id}"},
	"search-entities":             {http.MethodGet, "/api/v2/search"},
	"create-discovery":            {http.MethodPost, "/api/v2/discoveries"},
	"get-discovery":               {http.MethodGet, "/api/v2/discoveries/{id}"},
	"resolve-entity":              {http.MethodPost, "/api/v2/resolutions"},
	"entity-detail":               {http.MethodGet, "/api/v2/entities/{id}"},
	"refresh-entity":              {http.MethodPost, "/api/v2/entities/{id}/refreshes"},
	"job-status":                  {http.MethodGet, "/api/v2/jobs/{id}"},
	"public-changes":              {http.MethodGet, "/api/v2/changes"},
	"entity-credits":              {http.MethodGet, "/api/v2/entities/{id}/credits"},
	"entity-ratings":              {http.MethodGet, "/api/v2/entities/{id}/ratings"},
	"entity-images":               {http.MethodGet, "/api/v2/entities/{id}/images"},
	"entity-relations":            {http.MethodGet, "/api/v2/entities/{id}/relations"},
	"person-credits":              {http.MethodGet, "/api/v2/persons/{id}/credits"},
	"person-detail":               {http.MethodGet, "/api/v2/persons/{id}"},
	"image-original":              {http.MethodGet, "/api/v2/images/{id}"},
	"image-variant":               {http.MethodGet, "/api/v2/images/{id}/variants/{format}/{width}"},
	"discover-tv-show":            {http.MethodPost, "/api/v2/tv/discoveries"},
	"tv-show-detail":              {http.MethodGet, "/api/v2/tv/shows/{id}"},
	"season-detail":               {http.MethodGet, "/api/v2/seasons/{id}"},
	"episode-detail":              {http.MethodGet, "/api/v2/episodes/{id}"},
	"discover-anime":              {http.MethodPost, "/api/v2/anime/discoveries"},
	"anime-detail":                {http.MethodGet, "/api/v2/anime/{id}"},
	"discover-manga":              {http.MethodPost, "/api/v2/manga/discoveries"},
	"manga-detail":                {http.MethodGet, "/api/v2/manga/{id}"},
	"discover-manga-volume":       {http.MethodPost, "/api/v2/manga/volumes/discoveries"},
	"manga-volume-detail":         {http.MethodGet, "/api/v2/manga/volumes/{id}"},
	"discover-comic":              {http.MethodPost, "/api/v2/comics/discoveries"},
	"comic-volume-detail":         {http.MethodGet, "/api/v2/comics/volumes/{id}"},
	"recording-detail":            {http.MethodGet, "/api/v2/recordings/{id}"},
	"recording-fingerprints":      {http.MethodGet, "/api/v2/recordings/{id}/fingerprints"},
	"recording-lyrics":            {http.MethodGet, "/api/v2/recordings/{id}/lyrics"},
	"artist-top-tracks":           {http.MethodGet, "/api/v2/entities/{id}/top-tracks"},
	"match-recording-fingerprint": {http.MethodPost, "/api/v2/fingerprint-matches"},
	"get-fingerprint-match":       {http.MethodGet, "/api/v2/fingerprint-matches/{id}"},
	"release-detail":              {http.MethodGet, "/api/v2/releases/{id}"},
	"latest-library":              {http.MethodGet, "/api/v2/latest"},
	"browse-library":              {http.MethodGet, "/api/v2/browse"},
	"library-stats":               {http.MethodGet, "/api/v2/stats"},
	"collections-list":            {http.MethodGet, "/api/v2/collections"},
	"collection-detail":           {http.MethodGet, "/api/v2/collections/{id}"},
	"admin-jobs":                  {http.MethodGet, "/api/v2/admin/jobs"},
	"admin-job-action":            {http.MethodPost, "/api/v2/admin/jobs/actions"},
}

func TestDiscoveryControlFlowIsProviderTransparent(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	resolution := document.Components.Schemas["ResolutionInputBody"]
	if resolution == nil || resolution.Value == nil {
		t.Fatal("resolution request schema is missing")
	}
	if len(resolution.Value.Properties) != 2 { // candidate_ref plus Huma's read-only $schema
		t.Fatalf("resolution request leaked control fields: %v", schemaPropertyNames(resolution.Value))
	}
	if _, ok := resolution.Value.Properties["candidate_ref"]; !ok {
		t.Fatalf("resolution request has no opaque candidate_ref: %v", schemaPropertyNames(resolution.Value))
	}
	for _, forbidden := range []string{"kind", "provider", "namespace", "value"} {
		if _, ok := resolution.Value.Properties[forbidden]; ok {
			t.Errorf("resolution request exposes provider-shaped field %q", forbidden)
		}
	}

	candidate := document.Components.Schemas["Candidate"]
	if candidate == nil || candidate.Value == nil {
		t.Fatal("discovery candidate schema is missing")
	}
	if _, ok := candidate.Value.Properties["candidate_ref"]; !ok {
		t.Fatal("discovery candidates have no opaque candidate_ref")
	}
	for _, forbidden := range []string{"provider", "identity", "resolution", "existing_entity_id"} {
		if _, ok := candidate.Value.Properties[forbidden]; ok {
			t.Errorf("discovery candidate exposes provider routing field %q", forbidden)
		}
	}

	request := document.Components.Schemas["Request"]
	if request == nil || request.Value == nil || request.Value.Properties["identifiers"] == nil {
		t.Fatal("generic discovery cannot accept identifier evidence")
	}
	if operationByID(document, "person-credits") == nil || document.Paths.Value("/api/v2/persons/{provider}/{providerPersonId}/credits") != nil {
		t.Fatal("person filmography is not canonical-ID-only")
	}

	fingerprintCandidate := document.Components.Schemas["MatchCandidate"]
	if fingerprintCandidate == nil || fingerprintCandidate.Value == nil {
		t.Fatal("fingerprint match candidate schema is missing")
	}
	for _, required := range []string{"entity_id", "candidate_ref", "resolution_state"} {
		if _, ok := fingerprintCandidate.Value.Properties[required]; !ok {
			t.Errorf("fingerprint match candidates have no %q", required)
		}
	}
	for _, forbidden := range []string{"musicbrainz_id", "recording_id", "resolution"} {
		if _, ok := fingerprintCandidate.Value.Properties[forbidden]; ok {
			t.Errorf("fingerprint match candidate exposes provider routing field %q", forbidden)
		}
	}
}

func TestCanonicalRelationReferencesAreExplicit(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile(filepath.Join("..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, expectation := range []struct {
		schema string
		field  string
	}{
		{"EntityCredit", "person_entity_id"},
		{"CollectionCard", "id"},
		{"CollectionMember", "entity_id"},
		{"EntityRelation", "target_entity_id"},
		{"Episode", "id"},
		{"Episode", "season_id"},
		{"Season", "id"},
		{"ParentResource", "entity_id"},
		{"PersonCredit", "entity_id"},
		{"MatchCandidate", "entity_id"},
	} {
		schema := document.Components.Schemas[expectation.schema]
		if schema == nil || schema.Value == nil {
			t.Errorf("canonical relation schema %q is missing", expectation.schema)
			continue
		}
		property := schema.Value.Properties[expectation.field]
		if property == nil || property.Value == nil {
			t.Errorf("%s.%s is missing", expectation.schema, expectation.field)
			continue
		}
		if property.Value.Type == nil || !property.Value.Type.Is("string") || property.Value.Format != "uuid" {
			t.Errorf("%s.%s is not a Heya UUID", expectation.schema, expectation.field)
		}
	}

	credit := document.Components.Schemas["EntityCredit"].Value
	if !containsString(credit.Required, "person_entity_id") {
		t.Fatal("cast/crew credit person_entity_id is optional")
	}
	for _, schemaName := range []string{"CollectionMember", "EntityRelation", "PersonCredit", "MatchCandidate"} {
		schema := document.Components.Schemas[schemaName]
		if schema == nil || schema.Value == nil || schema.Value.Properties["resolution_state"] == nil {
			t.Errorf("%s does not expose unresolved relationship state", schemaName)
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func schemaPropertyNames(schema *openapi3.Schema) []string {
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		if name != "$schema" {
			names = append(names, name)
		}
	}
	return strings.Fields(strings.Join(names, " "))
}

func TestCheckedInOpenAPIContract(t *testing.T) {
	contract := filepath.Join("..", "api", "openapi.yaml")
	document, err := openapi3.NewLoader().LoadFromFile(contract)
	if err != nil {
		t.Fatalf("load %s: %v", contract, err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI: %v", err)
	}
	found := map[string]string{}
	for path, item := range document.Paths.Map() {
		for method, operation := range item.Operations() {
			if previous, exists := found[operation.OperationID]; exists {
				t.Fatalf("operation ID %q is duplicated by %s and %s %s", operation.OperationID, previous, method, path)
			}
			found[operation.OperationID] = fmt.Sprintf("%s %s", method, path)
		}
	}
	for operationID, expected := range publicOperations {
		actual, ok := found[operationID]
		if !ok {
			t.Errorf("missing public operation %q", operationID)
			continue
		}
		want := expected.method + " " + expected.path
		if actual != want {
			t.Errorf("operation %q: got %s, want %s", operationID, actual, want)
		}
		delete(found, operationID)
	}
	for operationID, route := range found {
		t.Errorf("uncatalogued public operation %q at %s", operationID, route)
	}
}

func TestAsynchronousSuccessResponsesAreTyped(t *testing.T) {
	contract := filepath.Join("..", "api", "openapi.yaml")
	document, err := openapi3.NewLoader().LoadFromFile(contract)
	if err != nil {
		t.Fatalf("load %s: %v", contract, err)
	}
	operationIDs := []string{
		"create-discovery",
		"discover-tv-show",
		"discover-anime",
		"discover-manga",
		"discover-manga-volume",
		"discover-comic",
		"resolve-entity",
		"refresh-entity",
		"match-recording-fingerprint",
		"image-original",
		"image-variant",
	}
	for _, operationID := range operationIDs {
		t.Run(operationID, func(t *testing.T) {
			operation := operationByID(document, operationID)
			if operation == nil {
				t.Fatalf("operation %q not found", operationID)
			}
			response := operation.Responses.Value("202")
			if response == nil {
				t.Fatalf("operation %q has no 202 response", operationID)
			}
			if response.Value == nil {
				t.Fatalf("operation %q has an unresolved 202 response", operationID)
			}
			mediaType := response.Value.Content.Get("application/json")
			if mediaType == nil || mediaType.Schema == nil || (mediaType.Schema.Ref == "" && mediaType.Schema.Value == nil) {
				t.Fatalf("operation %q has no typed 202 application/json body", operationID)
			}
		})
	}
}

func operationByID(document *openapi3.T, operationID string) *openapi3.Operation {
	for _, item := range document.Paths.Map() {
		for _, operation := range item.Operations() {
			if operation.OperationID == operationID {
				return operation
			}
		}
	}
	return nil
}
