CREATE TABLE image_cache_objects (
    object_key text PRIMARY KEY,
    media_type text NOT NULL,
    byte_size bigint NOT NULL CHECK (byte_size > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO image_cache_objects (object_key, media_type, byte_size, created_at)
SELECT object_key, media_type, byte_size, COALESCE(materialized_at, created_at)
FROM image_candidates
WHERE object_key IS NOT NULL AND media_type IS NOT NULL AND byte_size > 0
ON CONFLICT (object_key) DO NOTHING;

INSERT INTO image_cache_objects (object_key, media_type, byte_size, created_at)
SELECT object_key, media_type, byte_size, created_at
FROM image_variants
ON CONFLICT (object_key) DO NOTHING;

CREATE INDEX image_cache_objects_orphan_scan_idx
    ON image_cache_objects (created_at);
