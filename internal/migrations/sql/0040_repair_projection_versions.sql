-- Older publication projections used a fixed projection version without
-- advancing their owning entity. Restore the global invariant before all new
-- publication writes begin using transactional entity versions.
UPDATE entities AS entity
SET canonical_version = published.projection_version,
    updated_at = GREATEST(entity.updated_at, published.updated_at)
FROM (
    SELECT entity_id,
           max(projection_version) AS projection_version,
           max(updated_at) AS updated_at
    FROM api_documents
    GROUP BY entity_id
) AS published
WHERE entity.id = published.entity_id
  AND entity.canonical_version < published.projection_version;
