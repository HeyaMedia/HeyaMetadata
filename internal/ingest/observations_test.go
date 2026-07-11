package ingest

import "testing"

func TestBlobExtensionPreservesSourceEncoding(t *testing.T) {
	t.Parallel()
	if got := blobExtension("application/xml; charset=utf-8"); got != ".xml.gz" {
		t.Fatalf("XML extension: %s", got)
	}
	if got := blobExtension("application/json"); got != ".json.gz" {
		t.Fatalf("JSON extension: %s", got)
	}
}
