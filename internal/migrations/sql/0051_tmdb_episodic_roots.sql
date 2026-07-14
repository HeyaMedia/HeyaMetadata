-- TMDB is now the preferred screen root for movies, conventional TV, and
-- anime. Existing canonical UUIDs remain untouched; entities that already
-- carry an accepted TMDB TV claim are made immediately eligible for a
-- UUID-preserving root promotion refresh.
INSERT INTO provider_refresh_states (entity_id, provider, next_eligible_at)
SELECT DISTINCT claim.entity_id, 'tmdb', now()
FROM external_id_claims claim
WHERE claim.entity_kind IN ('tv_show', 'anime')
  AND claim.provider = 'tmdb'
  AND claim.namespace = 'tv'
  AND claim.state = 'accepted'
ON CONFLICT (entity_id, provider) DO UPDATE
SET next_eligible_at = LEAST(provider_refresh_states.next_eligible_at, EXCLUDED.next_eligible_at);

-- Completed searches retain private candidate routing. Rebuild episodic title
-- searches under the TMDB-first algorithm rather than allowing an old opaque
-- TVMaze/AniDB candidate to enqueue the former root.
DELETE FROM discovery_runs
WHERE kind IN ('tv_show', 'anime');
