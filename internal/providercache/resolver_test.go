package providercache

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestDecompressAcceptsGzipAndPlainBodies(t *testing.T) {
	t.Parallel()
	plain := []byte(`{"id":603}`)
	if got, err := decompress(plain); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("plain body: %q, %v", got, err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(plain)
	_ = writer.Close()
	if got, err := decompress(compressed.Bytes()); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("gzip body: %q, %v", got, err)
	}
}

func TestDecodeStoredBodyAcceptsWrappedAndGatewayDecodedPayloads(t *testing.T) {
	t.Parallel()
	// AniDB's provider payload is itself gzip. The storage layer wraps it in a
	// second gzip stream, while an S3 gateway may return either representation.
	providerPayload := gzipBody(t, []byte(`<animetitles/>`))
	expected := sha256.Sum256(providerPayload)
	checksum := hex.EncodeToString(expected[:])
	stored := gzipBody(t, providerPayload)

	for name, value := range map[string][]byte{
		"storage wrapper present": stored,
		"gateway decoded":         providerPayload,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeStoredBody(value, checksum)
			if err != nil || !bytes.Equal(got, providerPayload) {
				t.Fatalf("decoded=%x err=%v", got, err)
			}
		})
	}
}

func TestDecodeStoredBodyRejectsCorruption(t *testing.T) {
	t.Parallel()
	if _, err := decodeStoredBody([]byte("corrupt"), string(make([]byte, 64))); !errors.Is(err, errCorruptBody) {
		t.Fatalf("error: %v", err)
	}
}

func gzipBody(t *testing.T, body []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
