package golden

import (
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/server"
)

func TestValueAtPathTraversesArrays(t *testing.T) {
	t.Parallel()
	document := map[string]any{
		"data": map[string]any{
			"images": []any{
				map[string]any{"id": "first"},
				map[string]any{"id": "second"},
			},
		},
	}
	value, ok := valueAtPath(document, "data.images[].id")
	if !ok || emptyValue(value) {
		t.Fatalf("value=%v ok=%v", value, ok)
	}
	values, ok := value.([]any)
	if !ok || len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Fatalf("unexpected flattened values: %#v", value)
	}
}

func TestCapabilityOperationsExistInRouter(t *testing.T) {
	t.Parallel()
	document := server.New("golden-test").API().OpenAPI()
	for _, projection := range []string{"resolve", "job", "search", "change", "discovery", "fingerprint_match"} {
		operationID, ok := capabilityOperation(projection)
		if !ok {
			t.Fatalf("projection %s has no operation mapping", projection)
		}
		if !operationExists(document, operationID) {
			t.Errorf("operation %s is absent", operationID)
		}
	}
}

func TestStorefrontFreshnessRequiresCanonicalTargetAndProviderSource(t *testing.T) {
	t.Parallel()
	relations := []any{
		map[string]any{
			"target_entity_id": "release-group-id",
			"metadata": map[string]any{
				"title":   "Oi AG!",
				"sources": []any{map[string]any{"provider": "deezer"}},
			},
		},
	}
	if !arrayHasRelationTitleAndProvider(relations, "Oi AG!", "deezer") {
		t.Fatal("canonical storefront-backed release was not recognized")
	}
	if arrayHasRelationTitleAndProvider(relations, "Oi AG!", "apple") {
		t.Fatal("missing provider provenance was accepted")
	}
	delete(relations[0].(map[string]any), "target_entity_id")
	if arrayHasRelationTitleAndProvider(relations, "Oi AG!", "deezer") {
		t.Fatal("provider-only text without a canonical target was accepted")
	}
}
