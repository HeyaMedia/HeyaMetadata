-- The kindless /api/v2/latest shelf orders every search_entities row by
-- recency and takes 24; only the (kind, updated_at) index exists, so the
-- planner falls back to a parallel seq scan and top-N sort of the whole
-- table (~640ms warm, multi-second cold) on every edge-cache miss.
CREATE INDEX IF NOT EXISTS search_entities_updated_idx
    ON search_entities (updated_at DESC, display_title);
