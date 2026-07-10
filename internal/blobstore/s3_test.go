package blobstore

import "testing"

func TestContentKey(t *testing.T) {
	t.Parallel()

	checksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key, err := ContentKey(checksum, ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	want := "blobs/sha256/01/23/" + checksum + ".json.gz"
	if key != want {
		t.Fatalf("key: got %q, want %q", key, want)
	}
}

func TestContentKeyRejectsInvalidChecksum(t *testing.T) {
	t.Parallel()

	if _, err := ContentKey("not-a-checksum", ""); err == nil {
		t.Fatal("expected invalid checksum error")
	}
}
