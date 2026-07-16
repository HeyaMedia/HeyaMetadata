-- Lock only the canonical people shared by a credit refresh. Every caller
-- acquires the complete set in the same order, preventing reverse-order row
-- deadlocks without serializing unrelated movie and episodic projections.
CREATE OR REPLACE FUNCTION heya_lock_credit_people(
    requested_providers text[],
    requested_provider_person_ids text[]
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    person_lock_key text;
BEGIN
    IF cardinality(requested_providers) <> cardinality(requested_provider_person_ids) THEN
        RAISE EXCEPTION 'credit person lock arrays must have equal lengths';
    END IF;

    FOR person_lock_key IN
        WITH requested AS (
            SELECT DISTINCT
                lower(trim(input.provider)) AS provider,
                trim(input.provider_person_id) AS provider_person_id
            FROM unnest(requested_providers, requested_provider_person_ids)
                AS input(provider, provider_person_id)
            WHERE trim(input.provider) <> ''
              AND trim(input.provider_person_id) <> ''
        ), lock_roots AS (
            SELECT DISTINCT COALESCE(
                'canonical:' || claim.entity_id::text,
                'provider:' || requested.provider || ':' || requested.provider_person_id
            ) AS lock_key
            FROM requested
            LEFT JOIN external_id_claims claim
              ON claim.entity_kind = 'person'
             AND claim.provider = requested.provider
             AND claim.namespace = 'person'
             AND claim.normalized_value = requested.provider_person_id
             AND claim.state = 'accepted'
        )
        SELECT lock_key FROM lock_roots ORDER BY lock_key
    LOOP
        PERFORM pg_advisory_xact_lock(
            hashtextextended('heya:credit-person:' || person_lock_key, 0)
        );
    END LOOP;
END;
$$;

-- Migration 0060 used a single lock in this trigger as an immediate safety
-- measure. The writers now acquire deterministic per-person lock sets before
-- inserting, so the trigger can return to canonicalizing only the current row.
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
