ALTER TABLE provider_observations
    ADD COLUMN response_headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN failure_class text,
    ADD COLUMN request_fingerprint text;

ALTER TABLE source_blobs
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN deleted_at timestamptz;
CREATE INDEX source_blobs_expiry_idx ON source_blobs (expires_at)
    WHERE expires_at IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE entities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL,
    slug text NOT NULL,
    canonical_version bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (kind, slug)
);

CREATE TABLE entity_slugs (
    entity_id uuid NOT NULL REFERENCES entities(id),
    kind text NOT NULL,
    slug text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, slug)
);

CREATE TABLE external_id_claims (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id uuid NOT NULL REFERENCES entities(id),
    entity_kind text NOT NULL,
    provider text NOT NULL,
    namespace text NOT NULL,
    normalized_value text NOT NULL,
    state text NOT NULL DEFAULT 'accepted'
        CHECK (state IN ('proposed', 'accepted', 'rejected', 'superseded', 'disputed')),
    confidence double precision NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    source_observation_id uuid REFERENCES provider_observations(id),
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_kind, provider, namespace, normalized_value)
);
CREATE INDEX external_id_claims_entity_idx ON external_id_claims (entity_id, state);

CREATE TABLE external_id_conflicts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_kind text NOT NULL,
    claims jsonb NOT NULL,
    normalized_record_id uuid,
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'resolved', 'dismissed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE TABLE normalized_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id uuid REFERENCES entities(id),
    entity_kind text NOT NULL,
    provider text NOT NULL,
    provider_namespace text NOT NULL,
    provider_record_id text NOT NULL,
    primary_observation_id uuid NOT NULL REFERENCES provider_observations(id),
    supporting_observation_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    normalizer_version text NOT NULL,
    schema_version integer NOT NULL,
    document jsonb NOT NULL,
    warnings jsonb NOT NULL DEFAULT '[]'::jsonb,
    partial_failure boolean NOT NULL DEFAULT false,
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (primary_observation_id, normalizer_version, schema_version)
);
CREATE INDEX normalized_records_provider_idx
    ON normalized_records (entity_kind, provider, provider_namespace, provider_record_id, observed_at DESC);
CREATE INDEX normalized_records_entity_idx ON normalized_records (entity_id, observed_at DESC);

CREATE TABLE canonical_movies (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE image_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id uuid NOT NULL REFERENCES entities(id),
    provider text NOT NULL,
    provider_image_id text NOT NULL,
    class text NOT NULL,
    source_url text NOT NULL,
    language text,
    country text,
    width integer,
    height integer,
    provider_score double precision,
    source_observation_id uuid NOT NULL REFERENCES provider_observations(id),
    materialization_state text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_id, provider, provider_image_id, class)
);
CREATE INDEX image_candidates_entity_idx ON image_candidates (entity_id, class);

CREATE TABLE api_documents (
    entity_id uuid NOT NULL REFERENCES entities(id),
    document_kind text NOT NULL,
    schema_version integer NOT NULL,
    projection_version bigint NOT NULL,
    document jsonb NOT NULL,
    fresh_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_id, document_kind)
);

CREATE TABLE api_document_provenance (
    entity_id uuid NOT NULL REFERENCES entities(id),
    document_kind text NOT NULL,
    projection_version bigint NOT NULL,
    document jsonb NOT NULL,
    PRIMARY KEY (entity_id, document_kind),
    FOREIGN KEY (entity_id, document_kind) REFERENCES api_documents(entity_id, document_kind)
);

CREATE TABLE search_entities (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    kind text NOT NULL,
    slug text NOT NULL,
    display_title text NOT NULL,
    release_year integer,
    status text,
    genres text[] NOT NULL DEFAULT '{}',
    countries text[] NOT NULL DEFAULT '{}',
    languages text[] NOT NULL DEFAULT '{}',
    popularity double precision,
    summary jsonb NOT NULL,
    projection_version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX search_entities_kind_year_idx ON search_entities (kind, release_year);
CREATE INDEX search_entities_genres_idx ON search_entities USING gin (genres);

CREATE TABLE search_names (
    entity_id uuid NOT NULL REFERENCES entities(id),
    value text NOT NULL,
    normalized_value text NOT NULL,
    locale text NOT NULL DEFAULT '',
    name_type text NOT NULL,
    source_quality integer NOT NULL DEFAULT 0,
    PRIMARY KEY (entity_id, normalized_value, locale, name_type)
);
CREATE INDEX search_names_exact_idx ON search_names (normalized_value);
CREATE INDEX search_names_prefix_idx ON search_names (normalized_value text_pattern_ops);
CREATE INDEX search_names_trgm_idx ON search_names USING gin (normalized_value gin_trgm_ops);

CREATE TABLE provider_refresh_states (
    entity_id uuid NOT NULL REFERENCES entities(id),
    provider text NOT NULL,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_observation_id uuid REFERENCES provider_observations(id),
    failure_class text,
    failure_message text,
    current_job_id bigint,
    next_eligible_at timestamptz,
    PRIMARY KEY (entity_id, provider)
);

CREATE TABLE change_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id uuid NOT NULL REFERENCES entities(id),
    entity_kind text NOT NULL,
    slug text NOT NULL,
    scope text NOT NULL DEFAULT 'public',
    change_type text NOT NULL,
    changed_scopes text[] NOT NULL,
    projection_version bigint NOT NULL,
    provider_observation_id uuid REFERENCES provider_observations(id),
    river_job_id bigint,
    committed_at timestamptz NOT NULL DEFAULT now(),
    sequenced_at timestamptz
);
CREATE INDEX change_outbox_pending_idx ON change_outbox (committed_at, id) WHERE sequenced_at IS NULL;

CREATE TABLE change_cursor (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_sequence bigint NOT NULL DEFAULT 0
);
INSERT INTO change_cursor (singleton, last_sequence) VALUES (true, 0);

CREATE TABLE change_log (
    sequence bigint PRIMARY KEY,
    outbox_id uuid NOT NULL UNIQUE REFERENCES change_outbox(id),
    entity_id uuid NOT NULL REFERENCES entities(id),
    entity_kind text NOT NULL,
    slug text NOT NULL,
    scope text NOT NULL,
    change_type text NOT NULL,
    changed_scopes text[] NOT NULL,
    projection_version bigint NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE movie_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    tmdb_id bigint NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK (state IN ('working', 'completed', 'failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
