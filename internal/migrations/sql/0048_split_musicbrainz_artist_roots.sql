-- MusicBrainz artist entities are canonical identity roots. A supplemental
-- provider may relate several stage names or a former band name to one person,
-- but that crosswalk must not collapse distinct MusicBrainz artists.
CREATE TEMP TABLE artist_identity_splits (
    primary_mbid text NOT NULL,
    split_mbid text NOT NULL,
    preferred_slug text NOT NULL,
    source_entity_id uuid NOT NULL,
    target_entity_id uuid,
    PRIMARY KEY (primary_mbid, split_mbid)
) ON COMMIT DROP;

INSERT INTO artist_identity_splits(primary_mbid, split_mbid, preferred_slug, source_entity_id)
SELECT repair.primary_mbid, repair.split_mbid, repair.preferred_slug, source.entity_id
FROM (VALUES
    ('a74b1b7f-71a5-4011-9441-d0b5e4122711', 'c74f4726-2671-4011-81b6-f70da905c05a', 'on-a-friday'),
    ('f22942a1-6f70-4f48-866e-238cb2308fbd', 'f3def1b0-565b-44f5-81c5-d079dda8e65f', 'user18081971')
) AS repair(primary_mbid, split_mbid, preferred_slug)
JOIN LATERAL (
    SELECT candidate.entity_id
    FROM (
        SELECT claim.entity_id, 1 AS priority
        FROM external_id_claims claim
        WHERE claim.entity_kind='artist'
          AND claim.provider='musicbrainz'
          AND claim.namespace='artist'
          AND claim.normalized_value=repair.primary_mbid
          AND claim.state='accepted'
        UNION ALL
        SELECT record.entity_id, 2 AS priority
        FROM normalized_records record
        WHERE record.entity_kind='artist'
          AND record.provider='musicbrainz'
          AND record.provider_namespace='artist'
          AND record.provider_record_id=repair.primary_mbid
          AND record.entity_id IS NOT NULL
    ) candidate
    ORDER BY candidate.priority
    LIMIT 1
) source ON true
WHERE EXISTS (
    SELECT 1
    FROM normalized_records split_record
    WHERE split_record.entity_id=source.entity_id
      AND split_record.entity_kind='artist'
      AND split_record.provider='musicbrainz'
      AND split_record.provider_namespace='artist'
      AND split_record.provider_record_id=repair.split_mbid
);

DO $$
DECLARE
    repair record;
    target_id uuid;
    candidate_slug text;
    suffix integer;
BEGIN
    FOR repair IN SELECT * FROM artist_identity_splits LOOP
        SELECT claim.entity_id
        INTO target_id
        FROM external_id_claims claim
        WHERE claim.entity_kind='artist'
          AND claim.provider='musicbrainz'
          AND claim.namespace='artist'
          AND claim.normalized_value=repair.split_mbid
          AND claim.state='accepted'
          AND claim.entity_id<>repair.source_entity_id
        LIMIT 1;

        IF target_id IS NULL THEN
            suffix := 1;
            LOOP
                candidate_slug := repair.preferred_slug;
                IF suffix > 1 THEN
                    candidate_slug := candidate_slug || '-' || suffix::text;
                END IF;
                INSERT INTO entities(kind,slug)
                VALUES ('artist',candidate_slug)
                ON CONFLICT DO NOTHING
                RETURNING id INTO target_id;
                EXIT WHEN target_id IS NOT NULL;
                suffix := suffix + 1;
            END LOOP;
            INSERT INTO entity_slugs(entity_id,kind,slug)
            VALUES (target_id,'artist',candidate_slug);
        END IF;

        UPDATE artist_identity_splits
        SET target_entity_id=target_id
        WHERE primary_mbid=repair.primary_mbid AND split_mbid=repair.split_mbid;
    END LOOP;
END $$;

CREATE TEMP TABLE artist_identity_split_claims ON COMMIT DROP AS
SELECT DISTINCT
    repair.primary_mbid,
    repair.split_mbid,
    repair.source_entity_id,
    repair.target_entity_id,
    candidate->>'provider' AS provider,
    candidate->>'namespace' AS namespace,
    candidate->>'normalized_value' AS normalized_value
FROM artist_identity_splits repair
JOIN normalized_records record
  ON record.entity_id=repair.source_entity_id
 AND record.entity_kind='artist'
 AND record.provider='musicbrainz'
 AND record.provider_namespace='artist'
 AND record.provider_record_id=repair.split_mbid
CROSS JOIN LATERAL jsonb_array_elements(record.document->'identity_candidates') candidate
WHERE COALESCE(candidate->>'normalized_value','')<>'';

CREATE TEMP TABLE artist_identity_primary_claims ON COMMIT DROP AS
SELECT DISTINCT
    repair.primary_mbid,
    repair.source_entity_id,
    candidate->>'provider' AS provider,
    candidate->>'namespace' AS namespace,
    candidate->>'normalized_value' AS normalized_value
FROM artist_identity_splits repair
JOIN normalized_records record
  ON record.entity_id=repair.source_entity_id
 AND record.entity_kind='artist'
 AND record.provider='musicbrainz'
 AND record.provider_namespace='artist'
 AND record.provider_record_id=repair.primary_mbid
CROSS JOIN LATERAL jsonb_array_elements(record.document->'identity_candidates') candidate
WHERE COALESCE(candidate->>'normalized_value','')<>'';

-- Reassign every identity explicitly asserted by the split MusicBrainz root.
UPDATE external_id_claims claim
SET entity_id=split.target_entity_id,
    state='accepted',
    confidence=1,
    last_observed_at=now()
FROM artist_identity_split_claims split
WHERE claim.entity_kind='artist'
  AND claim.provider=split.provider
  AND claim.namespace=split.namespace
  AND claim.normalized_value=split.normalized_value;

-- Claims learned only from a broad supplemental crosswalk are no longer safe
-- unique identities for the primary artist.
UPDATE external_id_claims claim
SET state='disputed', last_observed_at=now()
FROM artist_identity_splits repair
WHERE claim.entity_id=repair.source_entity_id
  AND claim.entity_kind='artist'
  AND claim.state='accepted'
  AND NOT EXISTS (
      SELECT 1
      FROM artist_identity_primary_claims primary_claim
      WHERE primary_claim.primary_mbid=repair.primary_mbid
        AND primary_claim.provider=claim.provider
        AND primary_claim.namespace=claim.namespace
        AND primary_claim.normalized_value=claim.normalized_value
  );

CREATE TEMP TABLE artist_identity_split_observations ON COMMIT DROP AS
SELECT DISTINCT repair.source_entity_id, repair.target_entity_id, record.primary_observation_id
FROM artist_identity_splits repair
JOIN normalized_records record ON record.entity_id=repair.source_entity_id AND record.entity_kind='artist'
WHERE (
    record.provider='musicbrainz'
    AND record.provider_namespace='artist'
    AND record.provider_record_id=repair.split_mbid
) OR (
    record.provider IN ('lastfm','fanart')
    AND record.provider_record_id=repair.split_mbid
) OR EXISTS (
    SELECT 1
    FROM artist_identity_split_claims split
    WHERE split.split_mbid=repair.split_mbid
      AND split.provider=record.provider
      AND split.namespace=record.provider_namespace
      AND split.normalized_value=record.provider_record_id
);

UPDATE image_candidates image
SET entity_id=split.target_entity_id
FROM artist_identity_split_observations split
WHERE image.entity_id=split.source_entity_id
  AND image.source_observation_id=split.primary_observation_id;

UPDATE normalized_records record
SET entity_id=split.target_entity_id
FROM artist_identity_split_observations split
WHERE record.entity_id=split.source_entity_id
  AND record.primary_observation_id=split.primary_observation_id;

-- Move catalog history and the release relations produced from the split root.
INSERT INTO artist_catalog_promotions(artist_entity_id,release_group_entity_id,state,promoted_at,updated_at)
SELECT DISTINCT repair.target_entity_id, promotion.release_group_entity_id, promotion.state, promotion.promoted_at, now()
FROM artist_identity_splits repair
JOIN artist_catalog_promotions promotion ON promotion.artist_entity_id=repair.source_entity_id
WHERE promotion.release_group_entity_id IN (
    SELECT relation.target_entity_id
    FROM entity_relations relation
    JOIN provider_observations observation ON observation.id=relation.source_observation_id
    WHERE relation.source_entity_id=repair.source_entity_id
      AND observation.provider='musicbrainz'
      AND observation.provider_namespace='artist_release_groups'
      AND observation.provider_record_id=repair.split_mbid
)
ON CONFLICT(artist_entity_id,release_group_entity_id)
DO UPDATE SET state=EXCLUDED.state,updated_at=now();

DELETE FROM artist_catalog_promotions promotion
USING artist_identity_splits repair
WHERE promotion.artist_entity_id=repair.source_entity_id
  AND promotion.release_group_entity_id IN (
      SELECT relation.target_entity_id
      FROM entity_relations relation
      JOIN provider_observations observation ON observation.id=relation.source_observation_id
      WHERE relation.source_entity_id=repair.source_entity_id
        AND observation.provider='musicbrainz'
        AND observation.provider_namespace='artist_release_groups'
        AND observation.provider_record_id=repair.split_mbid
  );

INSERT INTO entity_relations(
    source_entity_id,target_entity_id,source_kind,target_kind,relation_type,
    provider,namespace,provider_value,position,metadata,state,
    source_observation_id,first_observed_at,last_observed_at
)
SELECT
    repair.target_entity_id,relation.target_entity_id,relation.source_kind,relation.target_kind,relation.relation_type,
    relation.provider,relation.namespace,relation.provider_value,relation.position,relation.metadata,'accepted',
    relation.source_observation_id,relation.first_observed_at,relation.last_observed_at
FROM artist_identity_splits repair
JOIN entity_relations relation ON relation.source_entity_id=repair.source_entity_id
JOIN provider_observations observation ON observation.id=relation.source_observation_id
WHERE observation.provider='musicbrainz'
  AND observation.provider_namespace='artist_release_groups'
  AND observation.provider_record_id=repair.split_mbid
ON CONFLICT(source_entity_id,relation_type,provider,namespace,provider_value)
DO UPDATE SET
    target_entity_id=EXCLUDED.target_entity_id,
    position=EXCLUDED.position,
    metadata=EXCLUDED.metadata,
    state='accepted',
    source_observation_id=EXCLUDED.source_observation_id,
    last_observed_at=EXCLUDED.last_observed_at;

DELETE FROM entity_relations relation
USING artist_identity_splits repair, provider_observations observation
WHERE relation.source_entity_id=repair.source_entity_id
  AND observation.id=relation.source_observation_id
  AND observation.provider='musicbrainz'
  AND observation.provider_namespace='artist_release_groups'
  AND observation.provider_record_id=repair.split_mbid;

UPDATE artist_catalog_sync_runs run
SET artist_entity_id=repair.target_entity_id
FROM artist_identity_splits repair
WHERE run.artist_entity_id=repair.source_entity_id
  AND run.musicbrainz_id=repair.split_mbid;

UPDATE artist_ingestion_runs run
SET entity_id=repair.target_entity_id
FROM artist_identity_splits repair
WHERE run.entity_id=repair.source_entity_id
  AND run.musicbrainz_id=repair.split_mbid::uuid;

-- Current Last.fm snapshots are scoped to the MB root used for their request.
DELETE FROM artist_top_tracks target
USING artist_identity_splits repair
WHERE target.artist_entity_id=repair.target_entity_id
  AND EXISTS (
      SELECT 1 FROM artist_top_tracks source
      JOIN provider_observations observation ON observation.id=source.source_observation_id
      WHERE source.artist_entity_id=repair.source_entity_id
        AND source.provider=target.provider
        AND observation.provider_record_id=repair.split_mbid
  );

DELETE FROM artist_top_track_snapshots target
USING artist_identity_splits repair
WHERE target.artist_entity_id=repair.target_entity_id
  AND EXISTS (
      SELECT 1 FROM artist_top_track_snapshots source
      JOIN provider_observations observation ON observation.id=source.source_observation_id
      WHERE source.artist_entity_id=repair.source_entity_id
        AND source.provider=target.provider
        AND observation.provider_record_id=repair.split_mbid
  );

UPDATE artist_top_tracks track
SET artist_entity_id=repair.target_entity_id
FROM artist_identity_splits repair, provider_observations observation
WHERE track.artist_entity_id=repair.source_entity_id
  AND observation.id=track.source_observation_id
  AND observation.provider_record_id=repair.split_mbid;

UPDATE artist_top_track_snapshots snapshot
SET artist_entity_id=repair.target_entity_id
FROM artist_identity_splits repair, provider_observations observation
WHERE snapshot.artist_entity_id=repair.source_entity_id
  AND observation.id=snapshot.source_observation_id
  AND observation.provider_record_id=repair.split_mbid;

DELETE FROM provider_refresh_states refresh
USING artist_identity_splits repair
WHERE refresh.entity_id IN (repair.source_entity_id, repair.target_entity_id);

-- Remove every derived representation; the durable jobs below rebuild both
-- sides from their own MusicBrainz spines and repopulate search/provenance.
DELETE FROM search_names search USING artist_identity_splits repair
WHERE search.entity_id IN (repair.source_entity_id, repair.target_entity_id);
DELETE FROM search_entities search USING artist_identity_splits repair
WHERE search.entity_id IN (repair.source_entity_id, repair.target_entity_id);
DELETE FROM api_document_provenance document USING artist_identity_splits repair
WHERE document.entity_id IN (repair.source_entity_id, repair.target_entity_id);
DELETE FROM api_documents document USING artist_identity_splits repair
WHERE document.entity_id IN (repair.source_entity_id, repair.target_entity_id);
DELETE FROM canonical_artists artist USING artist_identity_splits repair
WHERE artist.entity_id IN (repair.source_entity_id, repair.target_entity_id);

-- Completed recommendations contain opaque references to the corrupt entity.
-- Cache-key versioning in discovery/store.go makes their Redis copies
-- unreachable; deleting the durable rows also invalidates candidate refs.
DELETE FROM discovery_runs run
USING artist_identity_splits repair
WHERE run.document::text LIKE '%' || repair.source_entity_id::text || '%'
   OR run.document::text LIKE '%' || repair.target_entity_id::text || '%';

CREATE UNIQUE INDEX external_id_claims_one_accepted_musicbrainz_artist_root
ON external_id_claims(entity_id)
WHERE entity_kind='artist'
  AND provider='musicbrainz'
  AND namespace='artist'
  AND state='accepted';

INSERT INTO river_job(kind,args,max_attempts,priority,queue)
SELECT 'artist_ingest_v1', jsonb_build_object(
    'musicbrainz_id', root.mbid,
    'reason', 'musicbrainz_identity_split_rebuild'
), 5, 1, 'default'
FROM (
    SELECT primary_mbid AS mbid FROM artist_identity_splits
    UNION
    SELECT split_mbid AS mbid FROM artist_identity_splits
) root;
