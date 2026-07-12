CREATE TABLE fingerprint_match_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_hash text NOT NULL,
    raw_fingerprint bytea,
    acoustid_fingerprint text,
    duration_ms bigint NOT NULL CHECK(duration_ms > 0),
    state text NOT NULL CHECK(state IN('queued','working','completed','failed')),
    river_job_id bigint,
    document jsonb,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL DEFAULT now()+interval '1 hour'
);
CREATE INDEX fingerprint_match_runs_expiry_idx ON fingerprint_match_runs(expires_at);
CREATE UNIQUE INDEX fingerprint_match_runs_active_request_idx ON fingerprint_match_runs(request_hash) WHERE state IN('queued','working');
