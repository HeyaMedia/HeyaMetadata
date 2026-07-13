ALTER TABLE image_candidates
    ADD COLUMN last_accessed_at timestamptz,
    ADD COLUMN materialization_attempted_at timestamptz,
    ADD COLUMN evicted_at timestamptz,
    ADD COLUMN materialized_width integer,
    ADD COLUMN materialized_height integer;

CREATE INDEX image_candidates_cold_cache_idx
    ON image_candidates ((COALESCE(last_accessed_at, materialized_at)))
    WHERE materialization_state = 'ready';

CREATE TABLE image_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    image_id uuid NOT NULL REFERENCES image_candidates(id) ON DELETE CASCADE,
    transform_version text NOT NULL,
    format text NOT NULL CHECK (format IN ('webp', 'avif')),
    width integer NOT NULL CHECK (width > 0),
    height integer NOT NULL CHECK (height > 0),
    checksum text NOT NULL,
    object_key text NOT NULL,
    media_type text NOT NULL,
    byte_size bigint NOT NULL CHECK (byte_size > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (image_id, transform_version, format, width)
);

CREATE INDEX image_variants_image_idx
    ON image_variants (image_id, format, width);
