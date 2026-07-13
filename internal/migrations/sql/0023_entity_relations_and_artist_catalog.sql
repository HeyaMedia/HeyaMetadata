CREATE TABLE entity_relations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    target_entity_id uuid REFERENCES entities(id) ON DELETE SET NULL,
    source_kind text NOT NULL,
    target_kind text NOT NULL,
    relation_type text NOT NULL,
    provider text NOT NULL,
    namespace text NOT NULL,
    provider_value text NOT NULL,
    position integer,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    state text NOT NULL DEFAULT 'accepted' CHECK (state IN ('accepted','superseded')),
    source_observation_id uuid REFERENCES provider_observations(id),
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_entity_id, relation_type, provider, namespace, provider_value)
);

CREATE INDEX entity_relations_source_idx
    ON entity_relations (source_entity_id, relation_type, state, position);
CREATE INDEX entity_relations_target_idx
    ON entity_relations (target_entity_id, relation_type)
    WHERE target_entity_id IS NOT NULL;
CREATE INDEX entity_relations_provider_target_idx
    ON entity_relations (target_kind, provider, namespace, provider_value, state);

CREATE TABLE artist_catalog_sync_runs (
    river_job_id bigint PRIMARY KEY,
    artist_entity_id uuid NOT NULL REFERENCES entities(id),
    musicbrainz_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('working','completed','failed')),
    pages integer NOT NULL DEFAULT 0,
    release_groups integer NOT NULL DEFAULT 0,
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX artist_catalog_sync_runs_artist_idx
    ON artist_catalog_sync_runs (artist_entity_id, completed_at DESC);
