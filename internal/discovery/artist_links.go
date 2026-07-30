package discovery

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
)

const maxArtistRelationshipLookups = 3

type artistRelationshipCollector interface {
	Collect(context.Context, providers.Identifier) ([]providers.Payload, error)
}

// collectExplicitMusicBrainzArtistLinks expands only exact-name MusicBrainz
// search hits. Search responses deliberately omit URL relationships, but those
// relationships are authoritative identity evidence and must be considered
// before separately searched storefront roots are presented as ambiguity.
func collectExplicitMusicBrainzArtistLinks(ctx context.Context, collector artistRelationshipCollector, request Request, values []Candidate) (map[string][]ExternalID, []error) {
	result := map[string][]ExternalID{}
	var failures []error
	lookups := 0
	for _, candidate := range values {
		if candidate.Identity.Provider != "musicbrainz" || candidate.Identity.Namespace != "artist" || !artistCandidateExact(request.Query, candidate) {
			continue
		}
		if lookups >= maxArtistRelationshipLookups {
			break
		}
		lookups++
		mbid := strings.ToLower(strings.TrimSpace(candidate.Identity.Value))
		payloads, err := collector.Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: mbid})
		if err != nil {
			failures = append(failures, fmt.Errorf("collect MusicBrainz artist relationships for %s: %w", mbid, err))
			continue
		}
		if len(payloads) == 0 {
			failures = append(failures, fmt.Errorf("MusicBrainz artist relationships for %s returned no payload", mbid))
			continue
		}
		if payloads[0].StatusCode != http.StatusOK {
			failures = append(failures, &providers.StatusError{Provider: "musicbrainz", StatusCode: payloads[0].StatusCode})
			continue
		}
		record, err := musicbrainz.NormalizeArtist(payloads[0].Body, payloads[0].ObservationID, payloads[0].ObservedAt)
		if err != nil {
			failures = append(failures, fmt.Errorf("normalize MusicBrainz artist relationships for %s: %w", mbid, err))
			continue
		}
		for _, identity := range record.IdentityCandidates {
			if identity.Evidence != "musicbrainz_url_relationship" {
				continue
			}
			result[mbid] = appendUniqueExternalID(result[mbid], ExternalID{
				Provider:  identity.Provider,
				Namespace: identity.Namespace,
				Value:     identity.NormalizedValue,
			})
		}
	}
	return result, failures
}

func appendUniqueExternalID(values []ExternalID, value ExternalID) []ExternalID {
	key := artistRootKey(value)
	if key == "" {
		return values
	}
	for _, existing := range values {
		if artistRootKey(existing) == key {
			return values
		}
	}
	return append(values, value)
}

func artistRootKey(value ExternalID) string {
	provider := strings.ToLower(strings.TrimSpace(value.Provider))
	namespace := strings.ToLower(strings.TrimSpace(value.Namespace))
	identity := strings.TrimSpace(value.Value)
	if provider == "" || namespace == "" || identity == "" {
		return ""
	}
	return provider + "\x00" + namespace + "\x00" + identity
}

// consolidateExplicitMusicBrainzArtistLinks collapses storefront search hits
// only when one exact MusicBrainz artist explicitly links their provider roots.
// A storefront ID linked by multiple MusicBrainz artists remains ambiguous.
// Resolution stays on the MusicBrainz spine so the normal ingestion pipeline
// persists every relationship and enriches from every supported provider.
func consolidateExplicitMusicBrainzArtistLinks(values []Candidate, links map[string][]ExternalID) []Candidate {
	ownersByRoot := map[string][]int{}
	for index, candidate := range values {
		if candidate.Identity.Provider != "musicbrainz" || candidate.Identity.Namespace != "artist" {
			continue
		}
		mbid := strings.ToLower(strings.TrimSpace(candidate.Identity.Value))
		for _, link := range links[mbid] {
			key := artistRootKey(link)
			if key != "" {
				ownersByRoot[key] = appendUniqueIndex(ownersByRoot[key], index)
			}
		}
	}

	targetsByOwner := map[int][]int{}
	for index, candidate := range values {
		if candidate.Identity.Provider == "musicbrainz" {
			continue
		}
		owners := ownersByRoot[artistRootKey(candidate.Identity)]
		if len(owners) == 1 {
			targetsByOwner[owners[0]] = append(targetsByOwner[owners[0]], index)
		}
	}

	merged := map[int]Candidate{}
	removed := map[int]bool{}
	for owner, targets := range targetsByOwner {
		candidate := values[owner]
		linkedRoots := map[string]bool{}
		keepExisting := candidate.ExistingEntityID != ""
		for _, target := range targets {
			if target == owner {
				continue
			}
			root := artistRootKey(values[target].Identity)
			linkedRoots[root] = true
			candidate.Display = mergeArtistCandidateDisplay(candidate.Display, values[target].Display)
			candidate.ProviderScore = max(candidate.ProviderScore, values[target].ProviderScore)
			for _, hint := range values[target].MatchedReleases {
				candidate.MatchedReleases = appendUniqueReleaseHint(candidate.MatchedReleases, hint)
			}
			candidate.artistReleaseMatches = appendUniqueArtistReleaseMatches(candidate.artistReleaseMatches, values[target].artistReleaseMatches...)
			if values[target].ExistingEntityID == "" || values[target].ExistingEntityID != candidate.ExistingEntityID {
				keepExisting = false
			}
			removed[target] = true
		}
		if len(linkedRoots) == 0 {
			continue
		}
		if !keepExisting {
			// Selecting the combined candidate must run the MusicBrainz spine so
			// newly learned or cross-entity links are actually persisted.
			candidate.ExistingEntityID = ""
		}
		candidate.Confidence = .99
		candidate.Match = "strong"
		candidate.Evidence = append(candidate.Evidence, Evidence{
			Field:   "identity_crosswalk",
			Outcome: "explicit_relationship",
			Weight:  .39,
			Detail:  fmt.Sprintf("authoritative identity relationships link %d matching upstream roots", len(linkedRoots)),
		})
		merged[owner] = candidate
	}

	result := make([]Candidate, 0, len(values)-len(removed))
	for index, candidate := range values {
		if removed[index] {
			continue
		}
		if value, ok := merged[index]; ok {
			candidate = value
		}
		result = append(result, candidate)
	}
	return result
}

func appendUniqueIndex(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func mergeArtistCandidateDisplay(base, addition Display) Display {
	if base.Name == "" {
		base.Name = addition.Name
	}
	if base.SortName == "" {
		base.SortName = addition.SortName
	}
	if base.Disambiguation == "" {
		base.Disambiguation = addition.Disambiguation
	}
	if base.Type == "" {
		base.Type = addition.Type
	}
	if base.Country == "" {
		base.Country = addition.Country
	}
	if base.Area == "" {
		base.Area = addition.Area
	}
	if base.BeginDate == "" {
		base.BeginDate = addition.BeginDate
	}
	if base.EndDate == "" {
		base.EndDate = addition.EndDate
	}
	if base.Ended == nil {
		base.Ended = addition.Ended
	}
	base.Aliases = appendUniqueStrings(base.Aliases, addition.Aliases...)
	base.Genres = appendUniqueStrings(base.Genres, addition.Genres...)
	if addition.ImageURL != "" && (base.ImageURL == "" || addition.ImageWidth*addition.ImageHeight > base.ImageWidth*base.ImageHeight) {
		base.ImageURL = addition.ImageURL
		base.ImageWidth = addition.ImageWidth
		base.ImageHeight = addition.ImageHeight
	}
	base.ReleaseCount = max(base.ReleaseCount, addition.ReleaseCount)
	base.FanCount = max(base.FanCount, addition.FanCount)
	return base
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[strings.ToLower(strings.TrimSpace(value))] = true
	}
	for _, addition := range additions {
		key := strings.ToLower(strings.TrimSpace(addition))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, strings.TrimSpace(addition))
	}
	return values
}
