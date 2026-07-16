-- Credit projection refreshes can visit the same people in different orders.
-- Serializing canonical-person attachment prevents those transactions from
-- deadlocking while they upsert shared canonical_people rows. The transaction
-- scoped lock is re-entrant, so a refresh pays the lock cost only at its first
-- credit and retains it until the complete projection is committed.
CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    canonical_profile uuid;
BEGIN
    PERFORM pg_advisory_xact_lock(
        hashtextextended('heya:credit-projection-canonical-people', 0)
    );

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
