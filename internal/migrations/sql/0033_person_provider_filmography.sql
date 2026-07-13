CREATE TABLE person_provider_credits (
    id bigserial PRIMARY KEY,
    person_entity_id uuid NOT NULL REFERENCES canonical_people(entity_id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_target_id text NOT NULL,
    media_kind text NOT NULL CHECK (media_kind IN ('movie','tv_show')),
    title text NOT NULL,
    release_year integer,
    credit_type text NOT NULL CHECK (credit_type IN ('cast','crew')),
    character_name text NOT NULL DEFAULT '',
    department text NOT NULL DEFAULT '',
    job text NOT NULL DEFAULT '',
    credit_order integer NOT NULL DEFAULT 0,
    episode_count integer NOT NULL DEFAULT 0,
    image_id uuid,
    source_observation_id uuid REFERENCES provider_observations(id),
    observed_at timestamptz NOT NULL,
    UNIQUE(person_entity_id,provider,provider_target_id,credit_type,character_name,department,job)
);
CREATE INDEX person_provider_credits_person_year_idx
    ON person_provider_credits(person_entity_id,release_year DESC,credit_type,credit_order);

ALTER TABLE canonical_people
    ADD COLUMN known_for_department text,
    ADD COLUMN homepage text,
    ADD COLUMN popularity double precision;
