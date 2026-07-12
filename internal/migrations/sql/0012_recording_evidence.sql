CREATE TABLE recording_fingerprints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    algorithm text NOT NULL DEFAULT 'chromaprint',
    algorithm_version text NOT NULL,
    generator_version text NOT NULL,
    source_kind text NOT NULL DEFAULT 'provider_preview'
        CHECK (source_kind IN ('provider_preview')),
    source_provider text NOT NULL,
    source_track_id text NOT NULL,
    source_checksum text NOT NULL,
    fingerprint bytea,
    duration_ms bigint CHECK (duration_ms IS NULL OR duration_ms >= 0),
    hash_count integer NOT NULL DEFAULT 0 CHECK (hash_count >= 0),
    state text NOT NULL CHECK (state IN ('ready', 'failed')),
    failure_class text,
    failure_message text,
    retry_after timestamptz,
    generated_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((state = 'ready' AND fingerprint IS NOT NULL AND hash_count > 0)
        OR (state = 'failed' AND fingerprint IS NULL)),
    UNIQUE (source_provider, source_track_id, algorithm_version)
);
CREATE INDEX recording_fingerprints_recording_idx
    ON recording_fingerprints (recording_entity_id, state, generated_at DESC);

CREATE TABLE recording_lyrics (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_record_id text NOT NULL,
    track_name text NOT NULL,
    artist_name text NOT NULL,
    album_name text NOT NULL DEFAULT '',
    duration_ms bigint CHECK (duration_ms IS NULL OR duration_ms >= 0),
    instrumental boolean NOT NULL DEFAULT false,
    plain_lyrics text,
    synced_lyrics text,
    content_checksum text NOT NULL,
    source_observation_id uuid NOT NULL REFERENCES provider_observations(id),
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (recording_entity_id, provider, provider_record_id)
);
CREATE INDEX recording_lyrics_recording_idx
    ON recording_lyrics (recording_entity_id, observed_at DESC);
