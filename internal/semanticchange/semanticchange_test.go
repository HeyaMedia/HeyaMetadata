package semanticchange

import "testing"

func TestEqualIgnoresOperationalProjectionChurn(t *testing.T) {
	left := []byte(`{"projection_version":1,"display":{"title":"Same"},"freshness":{"updated_at":"old"},"provenance":{"detail":[{"observation_id":"a"}]}}`)
	right := []byte(`{"projection_version":2,"display":{"title":"Same"},"freshness":{"updated_at":"new"},"provenance":{"detail":[{"observation_id":"b"}]}}`)
	if !Equal(left, right) {
		t.Fatal("operational-only changes should compare equal")
	}
	if Equal(left, []byte(`{"projection_version":2,"display":{"title":"Changed"}}`)) {
		t.Fatal("public title change should not compare equal")
	}
}
