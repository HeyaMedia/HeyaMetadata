-- canonicalrefs.Resolve filters accepted claims by entity kind, provider,
-- namespace, and lower(normalized_value). No index covers that combination,
-- so the planner walks every claim for the provider/namespace pair and
-- filters thousands of rows to return a handful (~14ms per call at ~25k
-- musicbrainz artist claims, called continuously by music ingest jobs).
CREATE INDEX IF NOT EXISTS external_id_claims_accepted_ref_lookup_idx
    ON external_id_claims (entity_kind, provider, namespace, lower(normalized_value))
    WHERE state = 'accepted';

-- Issued-track coalescing joins release_tracks by release entity; without an
-- index every artist catalog sync sequentially scans the whole table, and the
-- table grows with each ingested release.
CREATE INDEX IF NOT EXISTS release_tracks_release_entity_idx
    ON release_tracks (release_entity_id);
