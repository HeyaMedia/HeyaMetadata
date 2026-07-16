-- Cold-cache maintenance only considers materialized images and orders by the
-- effective last-use timestamp. Index that exact predicate and expression so
-- the hourly sweep does not scan/sort millions of candidate rows.
CREATE INDEX IF NOT EXISTS image_candidates_ready_cold_access_idx
    ON image_candidates (
        (COALESCE(last_accessed_at, materialized_at, created_at)),
        id
    )
    WHERE materialization_state = 'ready';
