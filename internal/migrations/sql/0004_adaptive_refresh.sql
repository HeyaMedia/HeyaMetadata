CREATE TABLE entity_access_stats (
    entity_id uuid PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    total_fetches bigint NOT NULL DEFAULT 0,
    decayed_score double precision NOT NULL DEFAULT 0,
    last_accessed_at timestamptz,
    score_updated_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX provider_refresh_due_idx
    ON provider_refresh_states (next_eligible_at, provider)
    WHERE next_eligible_at IS NOT NULL;
