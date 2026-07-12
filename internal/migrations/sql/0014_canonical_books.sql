CREATE TABLE canonical_book_works (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE canonical_book_editions (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    work_entity_id uuid NOT NULL REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX canonical_book_editions_work_idx ON canonical_book_editions(work_entity_id);

CREATE TABLE canonical_authors (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE book_work_authors (
    work_entity_id uuid NOT NULL REFERENCES entities(id),
    author_entity_id uuid NOT NULL REFERENCES entities(id),
    position integer NOT NULL DEFAULT 0,
    PRIMARY KEY(work_entity_id, author_entity_id)
);

CREATE TABLE book_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    openlibrary_work_id text NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK(state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

