-- Artist identity is rooted in one MusicBrainz artist. Wikidata remains useful
-- descriptive evidence, but its cross-provider authority IDs can span stage
-- names, former projects, fictional members, and several distinct MB artists.
CREATE TEMP TABLE artist_authoritative_identity_claims ON COMMIT DROP AS
WITH latest_musicbrainz AS (
    SELECT DISTINCT ON (record.entity_id)
        record.entity_id,
        record.document
    FROM normalized_records record
    WHERE record.entity_kind='artist'
      AND record.provider='musicbrainz'
      AND record.provider_namespace='artist'
      AND record.entity_id IS NOT NULL
    ORDER BY record.entity_id, record.observed_at DESC, record.created_at DESC
)
SELECT DISTINCT
    root.entity_id,
    candidate->>'provider' AS provider,
    candidate->>'namespace' AS namespace,
    candidate->>'normalized_value' AS normalized_value
FROM latest_musicbrainz root
CROSS JOIN LATERAL jsonb_array_elements(root.document->'identity_candidates') candidate
WHERE COALESCE(candidate->>'normalized_value','')<>''
  AND COALESCE((candidate->>'confidence')::double precision,0)>=1;

CREATE TEMP TABLE artist_identity_scope_rebuilds ON COMMIT DROP AS
SELECT DISTINCT claim.entity_id
FROM external_id_claims claim
WHERE claim.entity_kind='artist'
  AND claim.state='accepted'
  AND NOT EXISTS (
      SELECT 1
      FROM artist_authoritative_identity_claims authoritative
      WHERE authoritative.entity_id=claim.entity_id
        AND authoritative.provider=claim.provider
        AND authoritative.namespace=claim.namespace
        AND authoritative.normalized_value=claim.normalized_value
  );

UPDATE external_id_claims claim
SET state='disputed', last_observed_at=now()
WHERE claim.entity_kind='artist'
  AND claim.state='accepted'
  AND EXISTS (
      SELECT 1 FROM artist_identity_scope_rebuilds rebuild
      WHERE rebuild.entity_id=claim.entity_id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM artist_authoritative_identity_claims authoritative
      WHERE authoritative.entity_id=claim.entity_id
        AND authoritative.provider=claim.provider
        AND authoritative.namespace=claim.namespace
        AND authoritative.normalized_value=claim.normalized_value
  );

-- Artist discovery decisions may have ranked polluted aliases or Wikidata
-- cross-identities. store.go's request-hash version makes Redis copies
-- unreachable; remove the durable runs and opaque candidate references too.
DELETE FROM discovery_runs WHERE kind='artist';

-- Reproject every known artist with the v2 Wikidata normalizer. Background
-- priority preserves interactive work and lets provider rate limiters pace the
-- one-time refresh.
INSERT INTO river_job(kind,args,max_attempts,priority,queue)
SELECT
    'artist_ingest_v1',
    jsonb_build_object(
        'musicbrainz_id', claim.normalized_value,
        'reason', 'artist_identity_scope_v2_full_refresh'
    ),
    5,
    4,
    'default'
FROM external_id_claims claim
WHERE claim.entity_kind='artist'
  AND claim.provider='musicbrainz'
  AND claim.namespace='artist'
  AND claim.state='accepted'
  AND NOT EXISTS (
      SELECT 1
      FROM river_job job
      WHERE job.kind='artist_ingest_v1'
        AND job.args->>'musicbrainz_id'=claim.normalized_value
        AND job.state IN ('available','pending','retryable','scheduled','running')
  );
