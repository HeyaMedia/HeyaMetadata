CREATE TABLE discovery_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_hash text NOT NULL UNIQUE,
    kind text NOT NULL,
    query text NOT NULL,
    request jsonb NOT NULL,
    state text NOT NULL CHECK (state IN ('queued','working','completed','failed')),
    river_job_id bigint,
    document jsonb,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL
);
CREATE INDEX discovery_runs_expiry_idx ON discovery_runs (expires_at);
