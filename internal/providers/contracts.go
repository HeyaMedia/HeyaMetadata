// Package providers defines the reusable boundary between provider collectors
// and domain-specific normalization and merge code.
package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type StatusError struct {
	Provider   string
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", e.Provider, e.StatusCode)
}

// HasHTTPStatus lets orchestration layers distinguish an expected missing
// supplemental record from an upstream failure without parsing error text.
func HasHTTPStatus(err error, status int) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == status
}

type Scope string

const (
	ScopeIdentity         Scope = "identity"
	ScopeTitles           Scope = "titles"
	ScopeDescriptions     Scope = "descriptions"
	ScopeClassification   Scope = "classification"
	ScopeReleases         Scope = "releases"
	ScopeRatings          Scope = "ratings"
	ScopeCredits          Scope = "credits"
	ScopeArtwork          Scope = "artwork"
	ScopeCollections      Scope = "collections"
	ScopeRecommendations  Scope = "recommendations"
	ScopeLyrics           Scope = "lyrics"
	ScopeVideos           Scope = "videos"
	ScopeFingerprints     Scope = "fingerprints"
	ScopeEpisodeNumbering Scope = "episode_numbering"
)

type Identifier struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type Capability struct {
	Provider            string
	EntityKind          string
	AcceptedIdentifiers []Identifier
	Provides            []Scope
	RawRetention        RetentionPolicy
	ResponseCache       ResponseCachePolicy
}

// ResponseCachePolicy controls whether an exact upstream request may be
// reused. It is deliberately independent from raw evidence retention.
type ResponseCachePolicy struct {
	ReuseDuration     time.Duration
	NegativeDuration  time.Duration
	RedisBodyDuration time.Duration
	MaxRedisBodyBytes int
}

func (p ResponseCachePolicy) DurationForStatus(status int) time.Duration {
	if status >= 200 && status < 300 {
		return p.ReuseDuration
	}
	if status == http.StatusNotFound {
		return p.NegativeDuration
	}
	return 0
}

type RetentionPolicy struct {
	Class        string
	Duration     time.Duration
	ObjectPrefix string
}

// Payload is one exact provider response. A collector may return several
// payloads for one logical record; each is persisted as its own observation.
type Payload struct {
	Provider          string
	ProviderNamespace string
	ProviderRecordID  string
	RequestKey        string
	StatusCode        int
	Headers           http.Header
	Body              []byte
	ObservedAt        time.Time
	ResponseTime      time.Duration
	ObservationID     string
	BlobChecksum      string
	FromCache         bool
	// ReuseDurationOverride lets a provider classify application-level errors
	// that were transported as HTTP success. Nil uses the capability policy;
	// a pointer to zero records the observation without reusing it.
	ReuseDurationOverride *time.Duration
}

func (p ResponseCachePolicy) DurationForPayload(payload Payload) time.Duration {
	if payload.ReuseDurationOverride != nil {
		return *payload.ReuseDurationOverride
	}
	return p.DurationForStatus(payload.StatusCode)
}

func RequestFingerprint(provider, requestKey string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + requestKey))
	return hex.EncodeToString(digest[:])
}

// PayloadResolver may satisfy a request from shared storage or invoke fetch.
// Implementations must not include provider credentials in their cache key.
type PayloadResolver interface {
	Resolve(context.Context, Payload, func() (Payload, error)) (Payload, error)
}

type Collector interface {
	Capability() Capability
	Collect(context.Context, Identifier) ([]Payload, error)
}
