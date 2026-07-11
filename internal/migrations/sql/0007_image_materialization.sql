ALTER TABLE image_candidates
    ADD COLUMN blob_checksum text,
    ADD COLUMN object_key text,
    ADD COLUMN media_type text,
    ADD COLUMN byte_size bigint,
    ADD COLUMN materialization_error text,
    ADD COLUMN materialized_at timestamptz,
    ADD CONSTRAINT image_candidates_materialization_state_check
        CHECK (materialization_state IN ('pending', 'working', 'ready', 'failed'));

CREATE INDEX image_candidates_materialization_idx
    ON image_candidates (materialization_state, created_at)
    WHERE materialization_state IN ('pending', 'failed');
