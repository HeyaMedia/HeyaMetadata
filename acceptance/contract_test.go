package acceptance_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
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
	"person-credits":              {http.MethodGet, "/api/v2/persons/{provider}/{providerPersonId}/credits"},
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
