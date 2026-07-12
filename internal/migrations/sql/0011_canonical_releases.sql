CREATE TABLE canonical_releases (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE canonical_recordings (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE release_media (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position > 0),
    title text NOT NULL DEFAULT '',
    format text NOT NULL DEFAULT '',
    track_count integer NOT NULL DEFAULT 0 CHECK (track_count >= 0),
    disc_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (release_entity_id, position)
);

CREATE TABLE release_tracks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    medium_id uuid NOT NULL REFERENCES release_media(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence > 0),
    position text NOT NULL DEFAULT '',
    number text NOT NULL DEFAULT '',
    title text NOT NULL,
    duration_ms bigint CHECK (duration_ms IS NULL OR duration_ms >= 0),
    recording_entity_id uuid REFERENCES entities(id),
    provider text NOT NULL,
    provider_track_id text NOT NULL,
    document jsonb NOT NULL,
    UNIQUE (medium_id, sequence),
    UNIQUE (provider, provider_track_id)
);
CREATE INDEX release_tracks_recording_idx ON release_tracks (recording_entity_id);

CREATE TABLE release_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    musicbrainz_id uuid NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK (state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
