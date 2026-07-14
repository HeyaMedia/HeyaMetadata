package recordings

import (
	"context"

	"github.com/HeyaMedia/HeyaMetadata/internal/canonicalrefs"
)

// LyricsAvailability resolves recording lyrics in one query so album and
// release tracklists never need to perform one lookup per track. Exact
// recording evidence includes synchronized lyrics. A recording can also be
// marked available when another explicit performance of the same MusicBrainz
// work has plain lyrics; timing data is never inherited across recordings.
func LyricsAvailability(ctx context.Context, db canonicalrefs.Querier, entityIDs []string) (map[string]bool, error) {
	available := map[string]bool{}
	unique := make([]string, 0, len(entityIDs))
	seen := map[string]bool{}
	for _, entityID := range entityIDs {
		if entityID == "" || seen[entityID] {
			continue
		}
		seen[entityID] = true
		unique = append(unique, entityID)
	}
	if len(unique) == 0 {
		return available, nil
	}
	rows, err := db.Query(ctx, `
		WITH requested(recording_entity_id) AS (
			SELECT unnest($1::uuid[])
		), available AS (
			SELECT lyrics.recording_entity_id
			FROM recording_lyrics lyrics
			WHERE lyrics.recording_entity_id=ANY($1::uuid[])
			  AND (lyrics.plain_lyrics IS NOT NULL OR lyrics.synced_lyrics IS NOT NULL)
			UNION
			SELECT own.source_entity_id
			FROM entity_relations own
			JOIN entity_relations peer
			  ON peer.source_kind='recording'
			 AND peer.target_kind=own.target_kind
			 AND peer.relation_type=own.relation_type
			 AND peer.provider=own.provider
			 AND peer.namespace=own.namespace
			 AND peer.provider_value=own.provider_value
			 AND peer.state='accepted'
			JOIN recording_lyrics lyrics
			  ON lyrics.recording_entity_id=peer.source_entity_id
			 AND lyrics.plain_lyrics IS NOT NULL
			WHERE own.source_entity_id=ANY($1::uuid[])
			  AND own.source_kind='recording'
			  AND own.target_kind='musical_work'
			  AND own.relation_type='performance_of'
			  AND own.provider='musicbrainz'
			  AND own.namespace='work'
			  AND own.state='accepted'
			  AND NOT (COALESCE(own.metadata->'attributes','[]'::jsonb) ? 'instrumental')
			  AND NOT EXISTS (
				SELECT 1 FROM recording_lyrics own_lyrics
				WHERE own_lyrics.recording_entity_id=own.source_entity_id
				  AND own_lyrics.instrumental
			  )
		)
		SELECT DISTINCT requested.recording_entity_id::text
		FROM requested
		JOIN available USING(recording_entity_id)`, unique)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			return nil, err
		}
		available[entityID] = true
	}
	return available, rows.Err()
}
