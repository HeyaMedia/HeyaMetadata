package golden

import "testing"

func TestValueAtPath(t *testing.T) {
	t.Parallel()
	document := map[string]any{"data": map[string]any{"names": []any{"Example"}}}
	value, ok := valueAtPath(document, "data.names")
	if !ok || emptyValue(value) {
		t.Fatalf("value=%v ok=%v", value, ok)
	}
	if _, ok := valueAtPath(document, "data.missing"); ok {
		t.Fatal("missing path resolved")
	}
}
