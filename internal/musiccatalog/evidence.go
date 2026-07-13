package musiccatalog

import (
	"context"
	"fmt"
	"strings"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
)

type trackEvidence struct {
	Provider   string
	ID         string
	Title      string
	DurationMS int64
	PreviewURL string
}
type detailEvidence struct {
	Barcode    string
	ISRCs      map[string]bool
	Tracks     []trackEvidence
	Tracklists [][]trackEvidence
}

func enrichClustersWithDetailEvidence(ctx context.Context, runtime *platform.Runtime, clusters []cluster, jobID int64) []cluster {
	hasCandidate := false
	for _, group := range clusters {
		if !canonicalSpineCluster(group) {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return clusters
	}
	cache := map[string]detailEvidence{}
	failed := map[string]bool{}
	remainingFetches := 100
	fingerprintMatcher := newFingerprintBridgeMatcher(runtime)
	load := func(source candidate) (detailEvidence, bool) {
		key := source.Provider + ":" + source.Namespace + ":" + source.ID
		if value, ok := cache[key]; ok {
			return value, true
		}
		if failed[key] {
			return detailEvidence{}, false
		}
		if remainingFetches <= 0 {
			return detailEvidence{}, false
		}
		remainingFetches--
		value, err := collectDetailEvidence(ctx, runtime, source, jobID)
		if err != nil {
			failed[key] = true
			return detailEvidence{}, false
		}
		cache[key] = value
		return value, true
	}
	result := make([]cluster, 0, len(clusters))
	for _, group := range clusters {
		if canonicalSpineCluster(group) {
			result = append(result, group)
			continue
		}
		matched := -1
		reason := ""
		confidence := 0.0
		ambiguous := false
		for anchorIndex := range result {
			if !canonicalSpineCluster(result[anchorIndex]) || !detailEvidenceCandidates(group, result[anchorIndex]) {
				continue
			}
			anchorReason, anchorConfidence, ok := clustersStronglyMatch(group, result[anchorIndex], load)
			if !ok {
				anchorReason, anchorConfidence, ok = fingerprintMatcher.matchCluster(ctx, group, result[anchorIndex], load)
			}
			if !ok {
				continue
			}
			if matched >= 0 {
				ambiguous = true
				break
			}
			matched, reason, confidence = anchorIndex, anchorReason, anchorConfidence
		}
		if matched < 0 || ambiguous {
			result = append(result, group)
			continue
		}
		for _, source := range group.Sources {
			if !hasSource(result[matched], source) {
				source.MatchReason = reason
				source.MatchConfidence = confidence
				result[matched].Sources = append(result[matched].Sources, source)
			}
		}
		result[matched].BridgeReason = reason
		result[matched].BridgeConfidence = confidence
	}
	return result
}

// Detail fetches are deliberately limited. Only compare release concepts that
// already agree by normalized title and year; track evidence may bridge a
// provider's EP/single classification disagreement, but it may not turn an
// unrelated catalog record into an identity candidate.
func detailEvidenceCandidates(left, right cluster) bool {
	for _, a := range left.Sources {
		for _, b := range right.Sources {
			if a.Provider == b.Provider {
				continue
			}
			ay, by := year(a.Date), year(b.Date)
			if ay > 0 && by > 0 && ay != by {
				continue
			}
			if textmatch.EquivalentRelease(a.Title, ay, b.Title, by) {
				return true
			}
		}
	}
	return false
}

func clustersStronglyMatch(left, right cluster, load func(candidate) (detailEvidence, bool)) (string, float64, bool) {
	bestReason := ""
	bestConfidence := 0.0
	for _, a := range left.Sources {
		if a.Provider == "lastfm" {
			continue
		}
		ae, ok := load(a)
		if !ok {
			continue
		}
		for _, b := range right.Sources {
			if b.Provider == "lastfm" || a.Provider == b.Provider {
				continue
			}
			be, ok := load(b)
			if !ok {
				continue
			}
			reason, confidence, matched := strongEvidenceMatch(ae, be)
			if matched && confidence > bestConfidence {
				bestReason, bestConfidence = reason, confidence
			}
		}
	}
	return bestReason, bestConfidence, bestReason != ""
}

func strongEvidenceMatch(a, b detailEvidence) (string, float64, bool) {
	if a.Barcode != "" && a.Barcode == b.Barcode {
		return "shared_barcode", .995, true
	}
	shared := 0
	for value := range a.ISRCs {
		if b.ISRCs[value] {
			shared++
		}
	}
	minimum := min(len(a.ISRCs), len(b.ISRCs))
	required := 2
	if minimum == 1 {
		required = 1
	}
	if shared >= required && minimum > 0 && shared*100/minimum >= 60 {
		return "shared_isrc_trackset", .995, true
	}
	for _, left := range evidenceTracklists(a) {
		for _, right := range evidenceTracklists(b) {
			if len(left) < 2 || len(left) != len(right) {
				continue
			}
			matched := 0
			durations := 0
			for i := range left {
				if textmatch.EquivalentRelease(left[i].Title, 0, right[i].Title, 0) {
					matched++
					if durationClose(left[i].DurationMS, right[i].DurationMS) {
						durations++
					}
				}
			}
			if matched*100/len(left) >= 80 && durations*100/len(left) >= 60 {
				return "ordered_tracklist_duration", .94, true
			}
		}
	}
	return "", 0, false
}
func durationClose(a, b int64) bool { return a == 0 || b == 0 || (a-b < 3000 && b-a < 3000) }

func evidenceTracklists(value detailEvidence) [][]trackEvidence {
	result := value.Tracklists
	if len(value.Tracks) > 0 {
		result = append(result, value.Tracks)
	}
	return result
}

func collectDetailEvidence(ctx context.Context, runtime *platform.Runtime, source candidate, jobID int64) (detailEvidence, error) {
	var record rgdomain.NormalizedRecordV1
	switch source.Provider {
	case "musicbrainz":
		return collectMusicBrainzDetailEvidence(ctx, runtime, source.ID)
	case "apple":
		base := apple.New(runtime.Config.Providers.Apple)
		r, err := resolver(runtime, base.Capability(), jobID)
		if err != nil {
			return detailEvidence{}, err
		}
		payload, err := apple.NewCached(runtime.Config.Providers.Apple, r, "").CollectITunesAlbum(ctx, source.ID)
		if err != nil {
			return detailEvidence{}, err
		}
		if payload.StatusCode != 200 {
			return detailEvidence{}, &providers.StatusError{Provider: "apple", StatusCode: payload.StatusCode}
		}
		record, err = apple.NormalizeAlbum(payload.Body, source.ID, payload.ObservationID, payload.ObservedAt)
		if err != nil {
			return detailEvidence{}, err
		}
	case "deezer":
		base := deezer.New(runtime.Config.Providers.Deezer)
		r, err := resolver(runtime, base.Capability(), jobID)
		if err != nil {
			return detailEvidence{}, err
		}
		payloads, err := deezer.NewCached(runtime.Config.Providers.Deezer, r).Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "album", Value: source.ID})
		if err != nil || len(payloads) == 0 {
			return detailEvidence{}, err
		}
		payload := payloads[0]
		if payload.StatusCode != 200 {
			return detailEvidence{}, &providers.StatusError{Provider: "deezer", StatusCode: payload.StatusCode}
		}
		record, err = deezer.NormalizeAlbum(payload.Body, payload.ObservationID, payload.ObservedAt)
		if err != nil {
			return detailEvidence{}, err
		}
	case "discogs":
		base := discogs.New(runtime.Config.Providers.Discogs)
		r, err := resolver(runtime, base.Capability(), jobID)
		if err != nil {
			return detailEvidence{}, err
		}
		payloads, err := discogs.NewCached(runtime.Config.Providers.Discogs, r, "").Collect(ctx, providers.Identifier{Provider: "discogs", Namespace: source.Namespace, Value: source.ID})
		if err != nil || len(payloads) == 0 {
			return detailEvidence{}, err
		}
		payload := payloads[0]
		if payload.StatusCode != 200 {
			return detailEvidence{}, &providers.StatusError{Provider: "discogs", StatusCode: payload.StatusCode}
		}
		if source.Namespace == "master" {
			record, err = discogs.NormalizeMaster(payload.Body, payload.ObservationID, payload.ObservedAt)
		} else {
			record, err = discogs.NormalizeRelease(payload.Body, payload.ObservationID, payload.ObservedAt)
		}
		if err != nil {
			return detailEvidence{}, err
		}
	default:
		return detailEvidence{}, fmt.Errorf("unsupported catalog detail provider %q", source.Provider)
	}
	return evidenceFromRecord(record), nil
}

func collectMusicBrainzDetailEvidence(ctx context.Context, runtime *platform.Runtime, releaseGroupID string) (detailEvidence, error) {
	value := detailEvidence{ISRCs: map[string]bool{}}
	rows, err := runtime.DB.Query(ctx, `
		SELECT edition.target_entity_id::text,track.sequence,track.title,COALESCE(track.duration_ms,0),
		       COALESCE(array_agg(DISTINCT claim.normalized_value) FILTER(WHERE claim.normalized_value IS NOT NULL),'{}')
		FROM external_id_claims release_group
		JOIN entity_relations edition ON edition.source_entity_id=release_group.entity_id
		 AND edition.relation_type='editions' AND edition.state='accepted' AND edition.target_entity_id IS NOT NULL
		JOIN release_tracks track ON track.release_entity_id=edition.target_entity_id
		LEFT JOIN external_id_claims claim ON claim.entity_id=track.recording_entity_id
		 AND claim.entity_kind='recording' AND claim.provider='isrc' AND claim.namespace='recording'
		WHERE release_group.entity_kind='release_group' AND release_group.provider='musicbrainz'
		  AND release_group.namespace='release_group' AND release_group.normalized_value=$1
		  AND release_group.state='accepted'
		GROUP BY edition.target_entity_id,track.id,track.sequence,track.title,track.duration_ms
		ORDER BY edition.target_entity_id,track.sequence`, strings.ToLower(strings.TrimSpace(releaseGroupID)))
	if err != nil {
		return detailEvidence{}, fmt.Errorf("load MusicBrainz issued track evidence: %w", err)
	}
	defer rows.Close()
	releaseIndexes := map[string]int{}
	for rows.Next() {
		var releaseID, title string
		var sequence int
		var duration int64
		var isrcs []string
		if err := rows.Scan(&releaseID, &sequence, &title, &duration, &isrcs); err != nil {
			return detailEvidence{}, err
		}
		index, ok := releaseIndexes[releaseID]
		if !ok {
			index = len(value.Tracklists)
			releaseIndexes[releaseID] = index
			value.Tracklists = append(value.Tracklists, []trackEvidence{})
		}
		value.Tracklists[index] = append(value.Tracklists[index], trackEvidence{Provider: "musicbrainz", Title: title, DurationMS: duration})
		for _, isrc := range isrcs {
			if isrc = strings.ToUpper(strings.TrimSpace(isrc)); isrc != "" {
				value.ISRCs[isrc] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return detailEvidence{}, err
	}
	if len(value.Tracklists) == 0 {
		return detailEvidence{}, fmt.Errorf("MusicBrainz release group %s has no materialized issued track evidence", releaseGroupID)
	}
	return value, nil
}

func evidenceFromRecord(record rgdomain.NormalizedRecordV1) detailEvidence {
	value := detailEvidence{ISRCs: map[string]bool{}}
	for _, edition := range record.Editions {
		if value.Barcode == "" {
			value.Barcode = normalizedBarcode(edition.Barcode)
		}
	}
	for _, track := range record.Tracks {
		isrc := strings.ToUpper(strings.TrimSpace(track.ISRC))
		if isrc != "" {
			value.ISRCs[isrc] = true
		}
		value.Tracks = append(value.Tracks, trackEvidence{Provider: record.ProviderRecord.Provider, ID: track.ProviderID, Title: track.Title, DurationMS: track.DurationMS, PreviewURL: track.PreviewURL})
	}
	return value
}
func normalizedBarcode(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return strings.TrimLeft(out.String(), "0")
}
