package musiccatalog

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
)

// A discovery request needs to prove only the release that connected an
// unclaimed MusicBrainz artist to an existing storefront artist. Walking the
// artist's complete discography here can take hours under provider rate limits
// and monopolizes the interactive discovery queue. The complete catalog sync
// remains durable background work after the artist identity is materialized.
const directArtistBridgeReleaseLimit = 5

// VerifyArtistIdentityBridge accepts the durable canonical-release proof when
// it already exists, then falls back to a bounded provider proof for the exact
// release supplied by discovery. Both paths require accepted ownership of the
// storefront artist root and an explicit MusicBrainz artist credit.
func VerifyArtistIdentityBridge(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge, jobID int64) (bool, error) {
	matched, err := PersistedArtistIdentityBridge(ctx, runtime, bridge)
	if err != nil || matched {
		return matched, err
	}
	return VerifyDirectArtistIdentityBridge(ctx, runtime, bridge, jobID)
}

// VerifyDirectArtistIdentityBridge proves one provider release bridge without
// mutating the public artist catalog. Provider payloads are still retained as
// observations, and artist materialization repeats this check before claiming
// the MusicBrainz root on the existing canonical entity.
func VerifyDirectArtistIdentityBridge(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge, jobID int64) (bool, error) {
	bridge = normalizeArtistIdentityBridge(bridge)
	if !validArtistIdentityBridge(bridge) {
		return false, nil
	}

	aliases, claims, err := artistContext(ctx, runtime, bridge.ArtistEntityID)
	if err != nil {
		return false, err
	}
	if !containsFold(claims[bridge.StorefrontProvider], bridge.StorefrontArtistID) {
		return false, nil
	}

	storefront, rootID, err := collectDirectStorefrontRelease(ctx, runtime, bridge, aliases, claims, jobID)
	if err != nil {
		return false, fmt.Errorf("collect direct storefront artist bridge: %w", err)
	}
	if rootID != bridge.StorefrontArtistID || storefront.ID == "" {
		return false, nil
	}
	storefrontEvidence, err := collectDetailEvidence(ctx, runtime, storefront, jobID)
	if err != nil {
		return false, fmt.Errorf("collect direct storefront release evidence: %w", err)
	}

	group, err := collectDirectMusicBrainzReleaseGroup(ctx, runtime, bridge, jobID)
	if err != nil {
		return false, err
	}
	if group.ProviderRecord.Value == "" || !directReleaseConceptCompatible(group, storefront) {
		return false, nil
	}

	// Release-group browse data already includes edition barcodes. A shared
	// barcode is provider-independent proof and avoids fetching any edition.
	if storefrontEvidence.Barcode != "" {
		for _, edition := range group.Editions {
			if normalizedBarcode(edition.Barcode) == storefrontEvidence.Barcode {
				return true, nil
			}
		}
	}

	for _, edition := range directBridgeEditions(group.Editions, storefront, storefrontEvidence) {
		musicBrainzEvidence, ok, err := collectDirectMusicBrainzReleaseEvidence(ctx, runtime, bridge, edition.ProviderID, jobID)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		if directReleaseEvidenceMatches(musicBrainzEvidence, storefrontEvidence) {
			return true, nil
		}
	}
	return false, nil
}

// Apple iTunes payloads frequently omit ISRCs and can report mastering
// durations several seconds away from an original MusicBrainz edition. Once
// the exact title/year and both provider artist credits have already matched,
// a complete ordered tracklist of meaningful length is itself hard release
// identity evidence. Short singles still require barcode, ISRC, or the stricter
// duration-bearing matcher.
func directReleaseEvidenceMatches(left, right detailEvidence) bool {
	if _, _, matched := strongEvidenceMatch(left, right); matched {
		return true
	}
	for _, a := range evidenceTracklists(left) {
		for _, b := range evidenceTracklists(right) {
			if len(a) < 4 || len(a) != len(b) {
				continue
			}
			matched := true
			for index := range a {
				if !textmatch.EquivalentRelease(a[index].Title, 0, b[index].Title, 0) {
					matched = false
					break
				}
			}
			if matched {
				return true
			}
		}
	}
	return false
}

func normalizeArtistIdentityBridge(bridge ArtistIdentityBridge) ArtistIdentityBridge {
	bridge.ArtistEntityID = strings.TrimSpace(bridge.ArtistEntityID)
	bridge.MusicBrainzArtistID = strings.ToLower(strings.TrimSpace(bridge.MusicBrainzArtistID))
	bridge.MusicBrainzReleaseGroupID = strings.ToLower(strings.TrimSpace(bridge.MusicBrainzReleaseGroupID))
	bridge.StorefrontProvider = strings.ToLower(strings.TrimSpace(bridge.StorefrontProvider))
	bridge.StorefrontArtistID = strings.TrimSpace(bridge.StorefrontArtistID)
	bridge.StorefrontReleaseNamespace = strings.ToLower(strings.TrimSpace(bridge.StorefrontReleaseNamespace))
	bridge.StorefrontReleaseID = strings.TrimSpace(bridge.StorefrontReleaseID)
	return bridge
}

func validArtistIdentityBridge(bridge ArtistIdentityBridge) bool {
	return bridge.ArtistEntityID != "" && bridge.MusicBrainzArtistID != "" && bridge.MusicBrainzReleaseGroupID != "" &&
		(bridge.StorefrontProvider == "apple" || bridge.StorefrontProvider == "deezer") &&
		bridge.StorefrontArtistID != "" && bridge.StorefrontReleaseNamespace == "album" && bridge.StorefrontReleaseID != ""
}

func collectDirectStorefrontRelease(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge, aliases []string, claims map[string][]string, jobID int64) (candidate, string, error) {
	switch bridge.StorefrontProvider {
	case "apple":
		return collectExactAppleRelease(ctx, runtime, bridge.StorefrontReleaseID, aliases, claims["apple"], jobID)
	case "deezer":
		return collectExactDeezerRelease(ctx, runtime, bridge.StorefrontReleaseID, aliases, claims["deezer"], jobID)
	default:
		return candidate{}, "", nil
	}
}

func collectDirectMusicBrainzReleaseGroup(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge, jobID int64) (rgdomain.NormalizedRecordV1, error) {
	base := musicbrainz.New(runtime.Config.Providers.MusicBrainz)
	resolver, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return rgdomain.NormalizedRecordV1{}, err
	}
	payloads, err := musicbrainz.NewCached(runtime.Config.Providers.MusicBrainz, resolver).Collect(ctx, providers.Identifier{
		Provider: "musicbrainz", Namespace: "release_group", Value: bridge.MusicBrainzReleaseGroupID,
	})
	if err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("collect direct MusicBrainz release group: %w", err)
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return rgdomain.NormalizedRecordV1{}, nil
	}
	if payloads[0].StatusCode != http.StatusOK {
		return rgdomain.NormalizedRecordV1{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: payloads[0].StatusCode}
	}
	record, err := musicbrainz.NormalizeReleaseGroup(payloads[0].Body, payloads[0].ObservationID, payloads[0].ObservedAt)
	if err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("normalize direct MusicBrainz release group: %w", err)
	}
	if record.ProviderRecord.Value != bridge.MusicBrainzReleaseGroupID || !releaseGroupCreditsArtist(record, bridge.MusicBrainzArtistID) {
		return rgdomain.NormalizedRecordV1{}, nil
	}
	return record, nil
}

func releaseGroupCreditsArtist(record rgdomain.NormalizedRecordV1, artistID string) bool {
	for _, credit := range record.ArtistCredits {
		if strings.EqualFold(credit.ArtistProvider, "musicbrainz") && strings.EqualFold(credit.ArtistNamespace, "artist") && strings.EqualFold(credit.ArtistID, artistID) {
			return true
		}
	}
	return false
}

func directReleaseConceptCompatible(group rgdomain.NormalizedRecordV1, storefront candidate) bool {
	if len(group.Titles) == 0 || strings.TrimSpace(storefront.Title) == "" {
		return false
	}
	title := group.Titles[0].Value
	for _, value := range group.Titles {
		if value.Primary {
			title = value.Value
			break
		}
	}
	groupYear := 0
	for _, value := range group.Dates {
		if candidateYear := year(value.Value); candidateYear > 0 && (groupYear == 0 || candidateYear < groupYear) {
			groupYear = candidateYear
		}
	}
	storefrontYear := year(storefront.Date)
	if groupYear > 0 && storefrontYear > 0 && groupYear != storefrontYear {
		return false
	}
	return textmatch.EquivalentRelease(title, groupYear, storefront.Title, storefrontYear)
}

func directBridgeEditions(editions []rgdomain.Edition, storefront candidate, evidence detailEvidence) []rgdomain.Edition {
	values := append([]rgdomain.Edition(nil), editions...)
	wantedYear := year(storefront.Date)
	wantedTracks := metadataInt(storefront.Metadata, "track_count")
	score := func(value rgdomain.Edition) int {
		result := 0
		if evidence.Barcode != "" && normalizedBarcode(value.Barcode) == evidence.Barcode {
			result += 100
		}
		if wantedTracks > 0 && value.TrackCount == wantedTracks {
			result += 20
		}
		if wantedYear > 0 && year(value.Date.Value) == wantedYear {
			result += 10
		}
		if strings.EqualFold(value.Status, "official") {
			result += 2
		}
		return result
	}
	sort.SliceStable(values, func(i, j int) bool {
		left, right := score(values[i]), score(values[j])
		if left != right {
			return left > right
		}
		return values[i].ProviderID < values[j].ProviderID
	})
	seen := map[string]bool{}
	result := make([]rgdomain.Edition, 0, min(len(values), directArtistBridgeReleaseLimit))
	for _, value := range values {
		id := strings.ToLower(strings.TrimSpace(value.ProviderID))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, value)
		if len(result) == directArtistBridgeReleaseLimit {
			break
		}
	}
	return result
}

func collectDirectMusicBrainzReleaseEvidence(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge, releaseID string, jobID int64) (detailEvidence, bool, error) {
	base := musicbrainz.New(runtime.Config.Providers.MusicBrainz)
	resolver, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return detailEvidence{}, false, err
	}
	payloads, err := musicbrainz.NewCached(runtime.Config.Providers.MusicBrainz, resolver).Collect(ctx, providers.Identifier{
		Provider: "musicbrainz", Namespace: "release", Value: releaseID,
	})
	if err != nil {
		return detailEvidence{}, false, fmt.Errorf("collect direct MusicBrainz release: %w", err)
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return detailEvidence{}, false, nil
	}
	if payloads[0].StatusCode != http.StatusOK {
		return detailEvidence{}, false, &providers.StatusError{Provider: "musicbrainz", StatusCode: payloads[0].StatusCode}
	}
	record, err := musicbrainz.NormalizeRelease(payloads[0].Body, payloads[0].ObservationID, payloads[0].ObservedAt)
	if err != nil {
		return detailEvidence{}, false, fmt.Errorf("normalize direct MusicBrainz release: %w", err)
	}
	if !musicBrainzReleaseBelongsToGroup(record, bridge.MusicBrainzReleaseGroupID) {
		return detailEvidence{}, false, nil
	}
	return evidenceFromMusicBrainzRelease(record), true, nil
}

func musicBrainzReleaseBelongsToGroup(record releasedomain.NormalizedRecord, releaseGroupID string) bool {
	for _, externalID := range record.ExternalIDs {
		if externalID.Provider == "musicbrainz" && externalID.Namespace == "release_group" && strings.EqualFold(externalID.Value, releaseGroupID) {
			return true
		}
	}
	return false
}

func evidenceFromMusicBrainzRelease(record releasedomain.NormalizedRecord) detailEvidence {
	value := detailEvidence{Barcode: normalizedBarcode(record.Barcode), ISRCs: map[string]bool{}}
	for _, medium := range record.Media {
		tracklist := make([]trackEvidence, 0, len(medium.Tracks))
		for _, track := range medium.Tracks {
			item := trackEvidence{Provider: "musicbrainz", ID: track.Recording.ProviderID, Title: track.Title, DurationMS: track.DurationMS}
			tracklist = append(tracklist, item)
			value.Tracks = append(value.Tracks, item)
			for _, isrc := range track.Recording.ISRCs {
				if isrc = strings.ToUpper(strings.TrimSpace(isrc)); isrc != "" {
					value.ISRCs[isrc] = true
				}
			}
		}
		if len(tracklist) > 0 {
			value.Tracklists = append(value.Tracklists, tracklist)
		}
	}
	return value
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}
