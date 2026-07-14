CREATE TABLE fingerprint_match_candidate_refs (
    candidate_ref uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fingerprint_match_run_id uuid NOT NULL REFERENCES fingerprint_match_runs(id) ON DELETE CASCADE,
    resolution_kind text NOT NULL,
    resolution_provider text NOT NULL,
    resolution_namespace text NOT NULL,
    resolution_value text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (
        fingerprint_match_run_id,
        resolution_kind,
        resolution_provider,
        resolution_namespace,
        resolution_value
    )
);

CREATE INDEX fingerprint_match_candidate_refs_expiry_idx
    ON fingerprint_match_candidate_refs (expires_at);
