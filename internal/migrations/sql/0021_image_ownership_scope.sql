ALTER TABLE image_candidates
    ADD COLUMN ownership_scope text NOT NULL DEFAULT 'entity';

UPDATE image_candidates
SET ownership_scope = CASE
    WHEN provider_image_id LIKE 'company:%' THEN 'company'
    WHEN provider_image_id LIKE 'credit:%' OR (class='profile' AND provider_image_id LIKE 'person:%') THEN 'credit'
    WHEN provider_image_id LIKE 'collection_member:%' THEN 'collection_member'
    WHEN provider_image_id LIKE 'collection_%' THEN 'collection'
    WHEN provider_image_id LIKE 'recommendation:%' THEN 'recommendation'
    ELSE 'entity'
END;

ALTER TABLE image_candidates
    ADD CONSTRAINT image_candidates_ownership_scope_check
    CHECK (ownership_scope IN ('entity','credit','company','collection','collection_member','recommendation'));

CREATE INDEX image_candidates_entity_artwork_idx
    ON image_candidates (entity_id, class)
    WHERE ownership_scope='entity';
