INSERT INTO entity_relations (
    source_entity_id, source_kind, target_kind, relation_type,
    provider, namespace, provider_value, metadata, state,
    first_observed_at, last_observed_at
)
SELECT
    rg.entity_id, 'release_group', 'release', 'editions',
    edition->>'provider', edition->>'namespace', edition->>'provider_id',
    edition, 'accepted', rg.updated_at, rg.updated_at
FROM canonical_release_groups rg
CROSS JOIN LATERAL jsonb_array_elements(
    CASE
        WHEN jsonb_typeof(rg.document #> '{data,editions}') = 'array'
            THEN rg.document #> '{data,editions}'
        ELSE '[]'::jsonb
    END
) AS edition
WHERE COALESCE(edition->>'provider', '') <> ''
  AND COALESCE(edition->>'namespace', '') <> ''
  AND COALESCE(edition->>'provider_id', '') <> ''
ON CONFLICT (source_entity_id, relation_type, provider, namespace, provider_value)
DO UPDATE SET metadata = EXCLUDED.metadata,
              state = 'accepted',
              last_observed_at = EXCLUDED.last_observed_at;

UPDATE entity_relations relation
SET target_entity_id = claim.entity_id
FROM external_id_claims claim
WHERE relation.target_kind = 'release'
  AND relation.state = 'accepted'
  AND claim.entity_kind = 'release'
  AND claim.state = 'accepted'
  AND claim.provider = relation.provider
  AND claim.namespace = relation.namespace
  AND claim.normalized_value = relation.provider_value;
