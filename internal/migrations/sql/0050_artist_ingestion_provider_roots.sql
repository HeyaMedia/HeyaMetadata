-- Artist ingestion is provider-independent. Keep the legacy MusicBrainz UUID
-- column for compatibility with old jobs and diagnostics, but record every
-- root in a common provider/ID pair so job polling works for Apple and Deezer.
ALTER TABLE artist_ingestion_runs
    ADD COLUMN provider text,
    ADD COLUMN provider_id text;

UPDATE artist_ingestion_runs
SET provider='musicbrainz', provider_id=musicbrainz_id::text
WHERE provider IS NULL OR provider_id IS NULL;

ALTER TABLE artist_ingestion_runs
    ALTER COLUMN musicbrainz_id DROP NOT NULL,
    ALTER COLUMN provider SET NOT NULL,
    ALTER COLUMN provider_id SET NOT NULL,
    ADD CONSTRAINT artist_ingestion_runs_provider_check
        CHECK (provider IN ('musicbrainz','apple','deezer')),
    ADD CONSTRAINT artist_ingestion_runs_provider_id_check
        CHECK (btrim(provider_id)<>'');

CREATE INDEX artist_ingestion_runs_provider_record_idx
    ON artist_ingestion_runs(provider,provider_id,started_at DESC);
