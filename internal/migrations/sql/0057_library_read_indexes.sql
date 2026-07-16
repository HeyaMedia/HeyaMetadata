-- Homepage and browse shelves order compact projections by kind and update
-- time. Without this index PostgreSQL reads and sorts every release group for
-- each request before it can return the first twelve rows.
CREATE INDEX IF NOT EXISTS search_entities_kind_updated_idx
    ON search_entities(kind, updated_at DESC, display_title);

-- Keep the aggregate statistics endpoint on narrow indexes instead of reading
-- the much wider entity, claim, artwork, and document rows.
CREATE INDEX IF NOT EXISTS entities_active_kind_idx
    ON entities(kind)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS external_id_claims_accepted_provider_idx
    ON external_id_claims(provider)
    WHERE state = 'accepted';

CREATE INDEX IF NOT EXISTS image_candidates_materialization_state_idx
    ON image_candidates(materialization_state);

CREATE INDEX IF NOT EXISTS api_documents_kind_fresh_idx
    ON api_documents(document_kind, fresh_until);
