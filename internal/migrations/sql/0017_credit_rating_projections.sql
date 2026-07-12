CREATE TABLE entity_credit_projections (
    id bigserial PRIMARY KEY,
    entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_person_id text NOT NULL,
    display_name text NOT NULL,
    credit_type text NOT NULL,
    character_name text,
    department text,
    job text,
    credit_order integer NOT NULL DEFAULT 0,
    profile_image_id uuid,
    projection_version bigint NOT NULL
);
CREATE INDEX entity_credit_projection_page_idx ON entity_credit_projections(entity_id,credit_type,credit_order,id);

CREATE TABLE entity_rating_projections (
    entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    system text NOT NULL,
    value double precision NOT NULL,
    scale_min double precision NOT NULL,
    scale_max double precision NOT NULL,
    votes bigint NOT NULL DEFAULT 0,
    projection_version bigint NOT NULL,
    PRIMARY KEY(entity_id,system)
);
