CREATE TABLE canonical_musical_works (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE musical_work_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    openopus_work_id bigint NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK (state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
