CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    canonical_profile uuid;
BEGIN
    NEW.person_entity_id := heya_ensure_canonical_person(
        NEW.provider,NEW.provider_person_id,NEW.display_name,NEW.profile_image_id
    );
    SELECT profile_image_id INTO canonical_profile
    FROM canonical_people WHERE entity_id=NEW.person_entity_id;
    UPDATE search_entities
    SET summary=jsonb_set(
        summary,'{display,image_id}',
        COALESCE(to_jsonb(canonical_profile::text),'null'::jsonb),true
    ),updated_at=now()
    WHERE entity_id=NEW.person_entity_id;
    RETURN NEW;
END;
$$;

UPDATE search_entities search
SET summary=jsonb_set(
    search.summary,'{display,image_id}',
    COALESCE(to_jsonb(person.profile_image_id::text),'null'::jsonb),true
),updated_at=now()
FROM canonical_people person
WHERE search.entity_id=person.entity_id;
