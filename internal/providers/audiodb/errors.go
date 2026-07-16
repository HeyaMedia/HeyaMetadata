package audiodb

import "errors"

// ErrNotFound represents a valid TheAudioDB response with no matching record.
// Sparse provider coverage is expected and must not make canonical ingestion
// partial or noisy.
var ErrNotFound = errors.New("TheAudioDB has no matching record")
