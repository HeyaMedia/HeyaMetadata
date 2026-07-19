package jobs

import (
	"context"
	"log/slog"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// enqueueUnresolvedRecordingArtists gives failed bounded materializations a
// normal River retry and newly materialized identities their first full
// supplemental-provider refresh. An existing Last.fm profile/scope attempt is
// enough to prove this is not merely the lightweight recording-credit root.
func enqueueUnresolvedRecordingArtists(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], recordingEntityID string) {
	detail, _, err := recordings.NewService(runtime).Detail(ctx, recordingEntityID)
	if err != nil {
		slog.WarnContext(ctx, "load recording credits for artist materialization", "recording_entity_id", recordingEntityID, "error", err)
		return
	}
	seen := map[string]bool{}
	for _, credit := range detail.Data.ArtistCredits {
		provider := strings.ToLower(strings.TrimSpace(credit.ArtistProvider))
		namespace := strings.ToLower(strings.TrimSpace(credit.ArtistNamespace))
		providerID := strings.ToLower(strings.TrimSpace(credit.ArtistID))
		if provider != "musicbrainz" || namespace != "artist" || providerID == "" || seen[providerID] {
			continue
		}
		seen[providerID] = true
		if credit.ArtistEntityID != "" {
			var enrichmentAttempted bool
			if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_refresh_states WHERE entity_id=$1 AND provider IN('lastfm','lastfm:top_tracks') AND last_attempt_at IS NOT NULL)`, credit.ArtistEntityID).Scan(&enrichmentAttempted); err != nil {
				slog.WarnContext(ctx, "check recording artist enrichment", "recording_entity_id", recordingEntityID, "artist_entity_id", credit.ArtistEntityID, "error", err)
				continue
			}
			if enrichmentAttempted {
				continue
			}
		}
		if _, err := InsertArtist(ctx, runtime, client, ArtistIngestArgs{Provider: provider, ProviderID: providerID, Reason: "recording_credit_materialization"}, PriorityInteractive); err != nil {
			slog.WarnContext(ctx, "enqueue recording artist credit", "recording_entity_id", recordingEntityID, "musicbrainz_artist_id", providerID, "error", err)
		}
	}
}

// enqueueRecordingsAwaitingArtist reprojects only recordings whose persisted
// credit relation still lacks this canonical artist. Their normal ingestion
// path updates the embedded credit, relation target, projection version, and
// change-feed scopes atomically.
func enqueueRecordingsAwaitingArtist(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], provider, providerID, artistEntityID string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if provider == "" || providerID == "" || artistEntityID == "" {
		return
	}
	rows, err := runtime.DB.Query(ctx, `SELECT DISTINCT claim.normalized_value FROM(SELECT relation.source_entity_id FROM entity_relations relation WHERE relation.source_kind='recording' AND relation.target_kind='artist' AND relation.relation_type='artist_credit' AND relation.provider=$1 AND relation.namespace='artist' AND relation.provider_value=$2 AND relation.state='accepted' AND relation.target_entity_id IS DISTINCT FROM $3::uuid UNION SELECT canonical.entity_id FROM canonical_recordings canonical WHERE (canonical.document#>'{data,artist_credits}') @> jsonb_build_array(jsonb_build_object('artist_provider',$1,'artist_namespace','artist','artist_id',$2)) AND EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(canonical.document#>'{data,artist_credits}','[]'::jsonb)) credit WHERE lower(COALESCE(credit->>'artist_provider',''))=$1 AND lower(COALESCE(credit->>'artist_namespace',''))='artist' AND lower(COALESCE(credit->>'artist_id',''))=$2 AND COALESCE(credit->>'artist_entity_id','')<>$3::text)) awaiting JOIN external_id_claims claim ON claim.entity_id=awaiting.source_entity_id AND claim.entity_kind='recording' AND claim.provider='musicbrainz' AND claim.namespace='recording' AND claim.state='accepted' JOIN entities recording ON recording.id=awaiting.source_entity_id AND recording.kind='recording' AND recording.deleted_at IS NULL ORDER BY claim.normalized_value`, provider, providerID, artistEntityID)
	if err != nil {
		slog.WarnContext(ctx, "find recordings awaiting artist credit", "provider", provider, "provider_id", providerID, "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var recordingMBID string
		if err := rows.Scan(&recordingMBID); err != nil {
			slog.WarnContext(ctx, "scan recording awaiting artist credit", "provider", provider, "provider_id", providerID, "error", err)
			return
		}
		if _, err := InsertRecording(ctx, runtime, client, RecordingIngestArgs{MusicBrainzID: recordingMBID, Reason: "artist_credit_materialized"}, PriorityInteractive); err != nil {
			slog.WarnContext(ctx, "enqueue recording credit reconciliation", "recording_mbid", recordingMBID, "artist_entity_id", artistEntityID, "error", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.WarnContext(ctx, "iterate recordings awaiting artist credit", "provider", provider, "provider_id", providerID, "error", err)
	}
}
