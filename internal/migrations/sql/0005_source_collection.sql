CREATE TABLE source_collection_runs (
    river_job_id bigint PRIMARY KEY,
    provider text NOT NULL,
    identifier_provider text NOT NULL,
    identifier_namespace text NOT NULL,
    identifier_value text NOT NULL,
    state text NOT NULL CHECK (state IN ('working', 'completed', 'failed')),
    observation_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    reused_count integer NOT NULL DEFAULT 0 CHECK (reused_count >= 0),
    recorded_count integer NOT NULL DEFAULT 0 CHECK (recorded_count >= 0),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX source_collection_runs_identifier_idx
    ON source_collection_runs (provider, identifier_provider, identifier_namespace, identifier_value, started_at DESC);
