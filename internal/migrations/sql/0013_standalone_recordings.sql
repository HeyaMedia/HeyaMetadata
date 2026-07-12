CREATE TABLE recording_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    musicbrainz_id uuid NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK (state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

INSERT INTO provider_refresh_states (entity_id, provider, next_eligible_at)
SELECT id, 'lrclib', now()
FROM entities
WHERE kind = 'recording' AND deleted_at IS NULL
ON CONFLICT (entity_id, provider) DO NOTHING;

WITH latest_lyrics AS (
    SELECT DISTINCT ON (recording_entity_id)
        recording_entity_id, source_observation_id, observed_at
    FROM recording_lyrics
    WHERE provider = 'lrclib'
    ORDER BY recording_entity_id, observed_at DESC
)
UPDATE provider_refresh_states refresh
SET last_attempt_at = latest.observed_at,
    last_success_at = latest.observed_at,
    last_observation_id = latest.source_observation_id,
    next_eligible_at = latest.observed_at + interval '30 days'
FROM latest_lyrics latest
WHERE refresh.entity_id = latest.recording_entity_id
  AND refresh.provider = 'lrclib';
