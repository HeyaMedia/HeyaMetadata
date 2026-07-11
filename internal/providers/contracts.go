// Package providers defines the reusable boundary between provider collectors
// and domain-specific normalization and merge code.
package providers

import (
	"context"
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

type Scope string

const (
	ScopeIdentity        Scope = "identity"
	ScopeTitles          Scope = "titles"
	ScopeDescriptions    Scope = "descriptions"
	ScopeClassification  Scope = "classification"
	ScopeReleases        Scope = "releases"
	ScopeRatings         Scope = "ratings"
	ScopeCredits         Scope = "credits"
	ScopeArtwork         Scope = "artwork"
	ScopeCollections     Scope = "collections"
	ScopeRecommendations Scope = "recommendations"
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
}

type Collector interface {
	Capability() Capability
	Collect(context.Context, Identifier) ([]Payload, error)
}
