-- TheXEM is now the private episode-structure authority for anime and supplies
-- alternate numbering for conventional TV. Make existing TMDB-rooted episodic
-- entities eligible for a UUID-preserving rebuild so deployed records gain the
-- new season/episode mapping without waiting for their normal refresh cadence.
UPDATE provider_refresh_states refresh
SET next_eligible_at = now()
FROM entities entity
WHERE refresh.entity_id = entity.id
  AND entity.kind IN ('tv_show', 'anime')
  AND entity.deleted_at IS NULL
  AND refresh.provider = 'tmdb';
