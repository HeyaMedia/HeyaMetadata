CREATE TABLE canonical_people (
    entity_id uuid PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    display_name text NOT NULL,
    profile_image_id uuid,
    biography text,
    birth_date date,
    death_date date,
    gender text,
    place_of_birth text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE entity_credit_projections
    ADD COLUMN person_entity_id uuid REFERENCES entities(id);

CREATE OR REPLACE FUNCTION heya_ensure_canonical_person(
    requested_provider text,
    requested_provider_person_id text,
    requested_display_name text,
    requested_profile_image_id uuid
) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE
    canonical_id uuid;
    canonical_slug text;
    normalized_provider text := lower(trim(requested_provider));
    normalized_person_id text := trim(requested_provider_person_id);
    clean_name text := trim(requested_display_name);
BEGIN
    SELECT entity_id INTO canonical_id
    FROM external_id_claims
    WHERE entity_kind='person' AND provider=normalized_provider
      AND namespace='person' AND normalized_value=normalized_person_id
      AND state='accepted';

    IF canonical_id IS NULL THEN
        canonical_slug := 'person-' || substr(md5(normalized_provider || ':' || normalized_person_id), 1, 24);
        INSERT INTO entities(kind,slug,canonical_version)
        VALUES('person',canonical_slug,1)
        RETURNING id INTO canonical_id;
        INSERT INTO entity_slugs(entity_id,kind,slug)
        VALUES(canonical_id,'person',canonical_slug);
        INSERT INTO external_id_claims(
            entity_id,entity_kind,provider,namespace,normalized_value,state,
            confidence,first_observed_at,last_observed_at
        ) VALUES(
            canonical_id,'person',normalized_provider,'person',normalized_person_id,
            'accepted',1,now(),now()
        );
    END IF;

    INSERT INTO canonical_people(entity_id,display_name,profile_image_id)
    VALUES(canonical_id,clean_name,requested_profile_image_id)
    ON CONFLICT(entity_id) DO UPDATE SET
        display_name=CASE
            WHEN canonical_people.display_name='' THEN EXCLUDED.display_name
            ELSE canonical_people.display_name
        END,
        profile_image_id=COALESCE(canonical_people.profile_image_id,EXCLUDED.profile_image_id),
        updated_at=now();

    SELECT slug INTO canonical_slug FROM entities WHERE id=canonical_id;
    INSERT INTO search_entities(entity_id,kind,slug,display_title,summary,projection_version)
    VALUES(
        canonical_id,'person',canonical_slug,clean_name,
        jsonb_build_object(
            'schema_version',1,'projection_version',1,'id',canonical_id,
            'kind','person','slug',canonical_slug,
            'display',jsonb_strip_nulls(jsonb_build_object(
                'title',clean_name,'image_id',requested_profile_image_id
            ))
        ),1
    ) ON CONFLICT(entity_id) DO UPDATE SET
        display_title=CASE WHEN search_entities.display_title='' THEN EXCLUDED.display_title ELSE search_entities.display_title END,
        summary=jsonb_set(
            search_entities.summary,
            '{display,image_id}',
            COALESCE(to_jsonb(requested_profile_image_id::text),search_entities.summary#>'{display,image_id}','null'::jsonb),
            true
        ),
        updated_at=now();

    IF clean_name <> '' THEN
        INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)
        VALUES(canonical_id,clean_name,lower(unaccent(clean_name)),'credit',70)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN canonical_id;
END;
$$;

CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.person_entity_id := heya_ensure_canonical_person(
        NEW.provider,NEW.provider_person_id,NEW.display_name,NEW.profile_image_id
    );
    RETURN NEW;
END;
$$;

CREATE TRIGGER entity_credit_attach_canonical_person
BEFORE INSERT OR UPDATE OF provider,provider_person_id,display_name,profile_image_id
ON entity_credit_projections
FOR EACH ROW EXECUTE FUNCTION heya_attach_canonical_person_to_credit();

UPDATE entity_credit_projections
SET person_entity_id=heya_ensure_canonical_person(
    provider,provider_person_id,display_name,profile_image_id
);

ALTER TABLE entity_credit_projections
    ALTER COLUMN person_entity_id SET NOT NULL;

CREATE INDEX entity_credit_person_filmography_idx
    ON entity_credit_projections(person_entity_id,entity_id,credit_type,credit_order);
