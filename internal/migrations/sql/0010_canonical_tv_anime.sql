CREATE TABLE canonical_tv_shows (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE canonical_anime (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE episodic_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    entity_kind text NOT NULL CHECK (entity_kind IN ('tv_show','anime')),
    provider text NOT NULL,
    namespace text NOT NULL,
    provider_id text NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK (state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
