CREATE INDEX external_id_claims_accepted_value_idx
    ON external_id_claims(lower(normalized_value),entity_id)
    WHERE state='accepted';

ALTER TABLE canonical_people
    ADD COLUMN normalized_display_name text;

UPDATE canonical_people
SET normalized_display_name=lower(unaccent(display_name));

ALTER TABLE canonical_people
    ALTER COLUMN normalized_display_name SET NOT NULL;

CREATE INDEX canonical_people_normalized_display_name_idx
    ON canonical_people(normalized_display_name,entity_id);

CREATE OR REPLACE FUNCTION heya_normalize_canonical_person_display_name()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.normalized_display_name := lower(unaccent(COALESCE(NEW.display_name,'')));
    RETURN NEW;
END;
$$;

CREATE TRIGGER canonical_people_normalize_display_name
BEFORE INSERT OR UPDATE OF display_name
ON canonical_people
FOR EACH ROW EXECUTE FUNCTION heya_normalize_canonical_person_display_name();
