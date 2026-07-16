-- Media projection should attach credits to established people, not rewrite
-- their canonical/search projections for every movie or episode refresh. Only
-- genuinely new provider identities need serialization and materialization.
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
    needs_projection boolean := false;
BEGIN
    SELECT claim.entity_id, person.entity_id IS NULL
    INTO canonical_id, needs_projection
    FROM external_id_claims claim
    LEFT JOIN canonical_people person ON person.entity_id = claim.entity_id
    WHERE claim.entity_kind = 'person'
      AND claim.provider = normalized_provider
      AND claim.namespace = 'person'
      AND claim.normalized_value = normalized_person_id
      AND claim.state = 'accepted';

    IF canonical_id IS NULL THEN
        -- Writers visit provider identities in deterministic order. Holding an
        -- exact-identity transaction lock makes concurrent first observation
        -- idempotent without serializing unrelated or established people.
        PERFORM pg_advisory_xact_lock(
            hashtextextended(
                'heya:new-credit-person:' || normalized_provider || ':' || normalized_person_id,
                0
            )
        );
        SELECT claim.entity_id, person.entity_id IS NULL
        INTO canonical_id, needs_projection
        FROM external_id_claims claim
        LEFT JOIN canonical_people person ON person.entity_id = claim.entity_id
        WHERE claim.entity_kind = 'person'
          AND claim.provider = normalized_provider
          AND claim.namespace = 'person'
          AND claim.normalized_value = normalized_person_id
          AND claim.state = 'accepted';

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
            needs_projection := true;
        END IF;
    END IF;

    IF needs_projection THEN
        IF canonical_slug IS NULL THEN
            SELECT slug INTO canonical_slug FROM entities WHERE id=canonical_id;
        END IF;
        INSERT INTO canonical_people(entity_id,display_name,profile_image_id)
        VALUES(canonical_id,clean_name,requested_profile_image_id)
        ON CONFLICT(entity_id) DO NOTHING;
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
        ) ON CONFLICT(entity_id) DO NOTHING;
        IF clean_name <> '' THEN
            INSERT INTO search_names(entity_id,value,normalized_value,name_type,source_quality)
            VALUES(canonical_id,clean_name,lower(unaccent(clean_name)),'credit',70)
            ON CONFLICT DO NOTHING;
        END IF;
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
