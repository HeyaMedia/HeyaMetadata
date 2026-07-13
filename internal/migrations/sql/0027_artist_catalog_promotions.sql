CREATE TABLE artist_catalog_promotions (
    artist_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    release_group_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','superseded')),
    promoted_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artist_entity_id, release_group_entity_id)
);

INSERT INTO artist_catalog_promotions (artist_entity_id, release_group_entity_id)
SELECT DISTINCT relation.source_entity_id, relation.target_entity_id
FROM entity_relations relation
WHERE relation.relation_type = 'discography'
  AND relation.target_entity_id IS NOT NULL
  AND relation.metadata->>'resolution_state' = 'promoted'
ON CONFLICT DO NOTHING;
