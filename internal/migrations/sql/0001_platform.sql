CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TABLE source_blobs (
    checksum text PRIMARY KEY CHECK (checksum ~ '^[a-f0-9]{64}$'),
    object_key text NOT NULL UNIQUE,
    compression text NOT NULL CHECK (compression IN ('none', 'gzip', 'zstd')),
    media_type text NOT NULL,
    uncompressed_size bigint NOT NULL CHECK (uncompressed_size >= 0),
    compressed_size bigint NOT NULL CHECK (compressed_size >= 0),
    integrity_state text NOT NULL DEFAULT 'verified'
        CHECK (integrity_state IN ('pending', 'verified', 'corrupt')),
    retention_class text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE provider_observations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL,
    provider_namespace text NOT NULL,
    provider_record_id text NOT NULL,
    request_key text NOT NULL,
    response_status integer,
    response_time_ms integer CHECK (response_time_ms IS NULL OR response_time_ms >= 0),
    observed_at timestamptz NOT NULL DEFAULT now(),
    blob_checksum text REFERENCES source_blobs(checksum),
    normalizer_version text NOT NULL,
    retention_class text NOT NULL,
    river_job_id bigint,
    warnings jsonb NOT NULL DEFAULT '[]'::jsonb,
    UNIQUE (provider, provider_namespace, provider_record_id, request_key, observed_at)
);

CREATE INDEX provider_observations_record_idx
    ON provider_observations (provider, provider_namespace, provider_record_id, observed_at DESC);
CREATE INDEX provider_observations_blob_idx
    ON provider_observations (blob_checksum)
    WHERE blob_checksum IS NOT NULL;

CREATE TABLE platform_smoke_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    river_job_id bigint NOT NULL UNIQUE,
    observation_id uuid NOT NULL REFERENCES provider_observations(id),
    blob_checksum text NOT NULL REFERENCES source_blobs(checksum),
    redis_roundtrip boolean NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now()
);
