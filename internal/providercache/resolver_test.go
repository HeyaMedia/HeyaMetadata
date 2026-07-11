package providercache

import (
	"bytes"
	"compress/gzip"
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
