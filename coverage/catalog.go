// Package coverage defines the executable semantic metadata coverage catalog.
package coverage

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed movie.json
var movieCatalogJSON []byte

//go:embed music.json
var musicCatalogJSON []byte

//go:embed books.json
var booksCatalogJSON []byte

//go:embed tv.json
var tvCatalogJSON []byte

//go:embed people.json
var peopleCatalogJSON []byte

// Catalog is a versioned collection of semantic metadata requirements.
type Catalog struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

// Entry describes one fact, relationship, or API capability that v2 must
// surface. Projection is a logical v2 path rather than a legacy JSON path.
type Entry struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	Category    string      `json:"category"`
	Description string      `json:"description"`
	Providers   []string    `json:"providers"`
	Projection  Projection  `json:"projection"`
	References  []Reference `json:"references"`
}

type Projection struct {
	Document string `json:"document"`
	Path     string `json:"path"`
}

// Reference identifies a real-world acceptance fixture. ExpectedProviders is
// the provenance that an integration check must observe, not merely providers
// that are theoretically able to supply the fact.
type Reference struct {
	Name              string            `json:"name"`
	EntityKind        string            `json:"reference_kind,omitempty"`
	ExternalIDs       map[string]string `json:"external_ids"`
	ExpectedProviders []string          `json:"expected_providers"`
}

// Movie parses and validates the embedded movie catalog.
func Movie() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(movieCatalogJSON, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode movie coverage catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Music parses and validates the embedded music catalog.
func Music() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(musicCatalogJSON, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode music coverage catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func Books() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(booksCatalogJSON, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode books coverage catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}
func TV() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(tvCatalogJSON, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode TV coverage catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}
func People() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(peopleCatalogJSON, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode people coverage catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Validate checks structural invariants needed by future API-backed coverage
// tests. It intentionally does not know about provider implementations.
func (c Catalog) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("coverage schema_version: got %d, want 1", c.SchemaVersion)
	}
	if len(c.Entries) == 0 {
		return fmt.Errorf("coverage catalog has no entries")
	}

	validCategories := map[string]bool{
		"fact": true, "relationship": true, "capability": true,
	}
	seen := make(map[string]struct{}, len(c.Entries))
	for i, entry := range c.Entries {
		prefix := fmt.Sprintf("entries[%d]", i)
		if entry.ID == "" || entry.Kind == "" || entry.Description == "" {
			return fmt.Errorf("%s: id, kind, and description are required", prefix)
		}
		if _, ok := seen[entry.ID]; ok {
			return fmt.Errorf("%s: duplicate id %q", prefix, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if !validCategories[entry.Category] {
			return fmt.Errorf("%s: unknown category %q", prefix, entry.Category)
		}
		if len(entry.Providers) == 0 {
			return fmt.Errorf("%s: providers are required", prefix)
		}
		if entry.Projection.Document == "" || entry.Projection.Path == "" {
			return fmt.Errorf("%s: projection document and path are required", prefix)
		}
		if len(entry.References) == 0 {
			return fmt.Errorf("%s: at least one reference is required", prefix)
		}

		providerSet := stringSet(entry.Providers)
		for j, reference := range entry.References {
			refPrefix := fmt.Sprintf("%s.references[%d]", prefix, j)
			if reference.Name == "" || len(reference.ExternalIDs) == 0 {
				return fmt.Errorf("%s: name and external_ids are required", refPrefix)
			}
			if len(reference.ExpectedProviders) == 0 {
				return fmt.Errorf("%s: expected_providers are required", refPrefix)
			}
			for _, provider := range reference.ExpectedProviders {
				if _, ok := providerSet[provider]; !ok {
					return fmt.Errorf("%s: expected provider %q is not declared by entry", refPrefix, provider)
				}
			}
		}
	}
	return nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

// IDs returns stable catalog entry IDs in sorted order for reporting tools.
func (c Catalog) IDs() []string {
	ids := make([]string, 0, len(c.Entries))
	for _, entry := range c.Entries {
		ids = append(ids, entry.ID)
	}
	sort.Strings(ids)
	return ids
}
