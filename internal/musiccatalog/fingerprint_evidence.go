package musiccatalog

import (
	"context"
	"sort"

	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprint"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
)

const catalogFingerprintBudget = 12

type fingerprintBridgeMatcher struct {
	runtime    *platform.Runtime
	calculator *fingerprint.Calculator
	remaining  int
	cache      map[string]string
}

func newFingerprintBridgeMatcher(runtime *platform.Runtime) *fingerprintBridgeMatcher {
	return &fingerprintBridgeMatcher{runtime: runtime, calculator: fingerprint.NewCalculator(runtime.Config.Chromaprint.FPCalcPath), remaining: catalogFingerprintBudget, cache: map[string]string{}}
}

func (m *fingerprintBridgeMatcher) matchCluster(ctx context.Context, candidateGroup, anchorGroup cluster, load func(candidate) (detailEvidence, bool)) (string, float64, bool) {
	anchorIDs, err := anchoredRecordingIDs(ctx, m.runtime, anchorGroup)
	if err != nil || len(anchorIDs) == 0 {
		anchorIDs = map[string]bool{}
	}
	for _, source := range anchorGroup.Sources {
		if detail, ok := load(source); ok {
			for _, track := range detail.Tracks {
				if id := m.existingRecording(ctx, track.Provider, track.ID); id != "" {
					anchorIDs[id] = true
				}
			}
		}
	}
	if len(anchorIDs) == 0 {
		return "", 0, false
	}
	tracks := []trackEvidence{}
	for _, source := range candidateGroup.Sources {
		if source.Provider == "lastfm" {
			continue
		}
		if detail, ok := load(source); ok {
			tracks = append(tracks, detail.Tracks...)
		}
	}
	if len(tracks) == 0 {
		return "", 0, false
	}
	matched := map[string]bool{}
	eligible := 0
	scoreTotal := 0.0
	for _, track := range tracks {
		if track.Provider == "" || track.ID == "" {
			continue
		}
		eligible++
		recordingID := m.existingRecording(ctx, track.Provider, track.ID)
		score := 1.0
		if recordingID == "" && track.PreviewURL != "" && m.remaining > 0 && m.calculator.Available() {
			recordingID, score = m.matchPreview(ctx, track)
		}
		if recordingID != "" && anchorIDs[recordingID] && !matched[recordingID] {
			matched[recordingID] = true
			scoreTotal += score
		}
	}
	confidence, ok := fingerprintCoverage(len(matched), eligible, scoreTotal)
	if !ok {
		return "", 0, false
	}
	return "chromaprint_recording_coverage", confidence, true
}

func fingerprintCoverage(matched, eligible int, scoreTotal float64) (float64, bool) {
	required := 2
	if eligible <= 2 {
		required = 1
	}
	if matched < required || eligible == 0 || matched*100/eligible < 50 {
		return 0, false
	}
	confidence := scoreTotal / float64(matched)
	return confidence, confidence >= .85
}

func (m *fingerprintBridgeMatcher) existingRecording(ctx context.Context, provider, trackID string) string {
	key := provider + ":" + trackID
	if value, ok := m.cache[key]; ok {
		return value
	}
	var id string
	_ = m.runtime.DB.QueryRow(ctx, `SELECT recording_entity_id::text FROM recording_fingerprints WHERE source_provider=$1 AND source_track_id=$2 AND state='ready' ORDER BY generated_at DESC LIMIT 1`, provider, trackID).Scan(&id)
	m.cache[key] = id
	return id
}

func anchoredRecordingIDs(ctx context.Context, runtime *platform.Runtime, group cluster) (map[string]bool, error) {
	targets, err := clusterTargets(ctx, runtime, group)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return map[string]bool{}, nil
	}
	rows, err := runtime.DB.Query(ctx, `SELECT DISTINCT track.recording_entity_id::text FROM entity_relations edition JOIN release_tracks track ON track.release_entity_id=edition.target_entity_id WHERE edition.source_entity_id=ANY($1::uuid[]) AND edition.relation_type='editions' AND edition.state='accepted' AND edition.target_entity_id IS NOT NULL AND track.recording_entity_id IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (m *fingerprintBridgeMatcher) matchPreview(ctx context.Context, track trackEvidence) (string, float64) {
	m.remaining--
	previewURL := track.PreviewURL
	if track.Provider == "deezer" {
		payloads, err := deezer.New(m.runtime.Config.Providers.Deezer).Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "track", Value: track.ID})
		if err != nil || len(payloads) == 0 || payloads[0].StatusCode != 200 {
			return "", 0
		}
		previewURL, err = deezer.TrackPreviewURL(payloads[0].Body, track.ID)
		if err != nil {
			return "", 0
		}
	}
	generated, err := m.calculator.FromURL(ctx, previewURL)
	if err != nil {
		return "", 0
	}
	packed := fingerprint.Pack(generated.Hashes)
	tokens := fingerprint.LandmarkTokens(packed)
	rows, err := m.runtime.DB.Query(ctx, `SELECT f.recording_entity_id::text,f.fingerprint FROM recording_fingerprints f WHERE f.state='ready' AND f.id IN(SELECT fingerprint_id FROM recording_fingerprint_landmarks WHERE token=ANY($1) GROUP BY fingerprint_id ORDER BY count(*) DESC LIMIT 100)`, tokens)
	if err != nil {
		return "", 0
	}
	defer rows.Close()
	type hit struct {
		id    string
		score float64
	}
	hits := []hit{}
	query := fingerprint.Fingerprint{Hashes: generated.Hashes, Duration: generated.Duration}
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		matched := fingerprint.Match(query, fingerprint.Fingerprint{Hashes: fingerprint.Unpack(raw)})
		if matched.Match {
			hits = append(hits, hit{id: id, score: 1 - matched.BitError})
		}
	}
	if len(hits) == 0 {
		return "", 0
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if hits[0].score < .85 {
		return "", 0
	}
	if len(hits) > 1 && hits[0].id != hits[1].id && hits[0].score-hits[1].score < .03 {
		return "", 0
	}
	m.cache[track.Provider+":"+track.ID] = hits[0].id
	return hits[0].id, hits[0].score
}
