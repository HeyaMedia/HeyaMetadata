ALTER TABLE artist_catalog_sync_runs
    ADD COLUMN sync_version text NOT NULL DEFAULT 'musicbrainz-artist-catalog/v1';

CREATE INDEX artist_catalog_sync_runs_version_idx
    ON artist_catalog_sync_runs (artist_entity_id, sync_version, completed_at DESC);
