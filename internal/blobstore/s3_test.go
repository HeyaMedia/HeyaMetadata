package blobstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

func TestContentKey(t *testing.T) {
	t.Parallel()

	checksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key, err := ContentKey("data", checksum, ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	want := "data/blobs/sha256/01/23/" + checksum + ".json.gz"
	if key != want {
		t.Fatalf("key: got %q, want %q", key, want)
	}
}

func TestContentKeyUnderRetentionPrefix(t *testing.T) {
	t.Parallel()
	checksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	store := &Store{prefix: "data"}
	key, err := store.ContentKeyUnder("ephemeral/48h", checksum, ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	want := "data/ephemeral/48h/blobs/sha256/01/23/" + checksum + ".json.gz"
	if key != want {
		t.Fatalf("key: got %q, want %q", key, want)
	}
}

func TestStoreUsesSignedPathStyleRequests(t *testing.T) {
	t.Parallel()
	var stored []byte
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Authorization"), "Credential=access/") {
			t.Errorf("request was not signed with configured access key")
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/heyamedia/data/.readiness":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `<Error><Code>NoSuchKey</Code></Error>`)
		case request.Method == http.MethodPut && request.URL.Path == "/heyamedia/data/object":
			stored, _ = io.ReadAll(request.Body)
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodGet && request.URL.Path == "/heyamedia/data/object":
			_, _ = writer.Write(stored)
		case request.Method == http.MethodDelete && request.URL.Path == "/heyamedia/data/object":
			deleted = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store, err := New(context.Background(), config.S3Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "heyamedia", Prefix: "data",
		AccessKeyID: "access", SecretAccessKey: "secret", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := store.PutImmutable(context.Background(), "data/object", []byte("payload"), "text/plain", ""); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := store.Get(context.Background(), "data/object")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("body: got %q", body)
	}
	if err := store.Delete(context.Background(), "data/object"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete request was not sent")
	}
}

func TestContentKeyRejectsInvalidChecksum(t *testing.T) {
	t.Parallel()

	if _, err := ContentKey("data", "not-a-checksum", ""); err == nil {
		t.Fatal("expected invalid checksum error")
	}
}
