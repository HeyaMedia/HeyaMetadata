ALTER TABLE image_candidates
    DROP CONSTRAINT image_candidates_ownership_scope_check;
ALTER TABLE image_candidates
    ADD CONSTRAINT image_candidates_ownership_scope_check
    CHECK (ownership_scope IN (
        'entity','credit','company','collection','collection_member','recommendation',
        'person_profile','person_credit'
    ));
CREATE INDEX image_candidates_person_profile_idx
    ON image_candidates(entity_id,class)
    WHERE ownership_scope='person_profile';
