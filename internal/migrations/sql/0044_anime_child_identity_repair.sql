CREATE TEMP TABLE anime_episode_identity_repairs ON COMMIT DROP AS
SELECT legacy.id AS legacy_id, claimed.episode_id AS replacement_id
FROM episodic_episodes legacy
JOIN entities show ON show.id=legacy.show_entity_id AND show.kind='anime'
JOIN episodic_episode_external_ids claimed
  ON claimed.show_entity_id=legacy.show_entity_id
 AND claimed.provider='anidb'
 AND claimed.namespace='episode'
 AND claimed.normalized_value=lower(legacy.document->>'provider_id')
 AND claimed.episode_id<>legacy.id
WHERE COALESCE(jsonb_array_length(legacy.document->'external_ids'),0)=0
  AND COALESCE(legacy.document->>'provider_id','')<>'';

UPDATE image_candidates image
SET owner_resource_id=repair.legacy_id
FROM anime_episode_identity_repairs repair
WHERE image.owner_resource_id=repair.replacement_id;

UPDATE episodic_episodes legacy
SET season_id=replacement.season_id,
    document=jsonb_set(replacement.document,'{id}',to_jsonb(legacy.id::text)),
    updated_at=now()
FROM anime_episode_identity_repairs repair
JOIN episodic_episodes replacement ON replacement.id=repair.replacement_id
WHERE legacy.id=repair.legacy_id;

DELETE FROM episodic_episode_external_ids external
USING anime_episode_identity_repairs repair
WHERE external.episode_id=repair.legacy_id;

UPDATE episodic_episode_external_ids external
SET episode_id=repair.legacy_id
FROM anime_episode_identity_repairs repair
WHERE external.episode_id=repair.replacement_id;

DELETE FROM episodic_episodes episode
USING anime_episode_identity_repairs repair
WHERE episode.id=repair.replacement_id;

INSERT INTO episodic_episode_external_ids(episode_id,show_entity_id,provider,namespace,normalized_value)
SELECT episode.id,episode.show_entity_id,'anidb','episode',lower(episode.document->>'provider_id')
FROM episodic_episodes episode
JOIN entities show ON show.id=episode.show_entity_id AND show.kind='anime'
WHERE COALESCE(episode.document->>'provider_id','')<>''
  AND COALESCE(jsonb_array_length(episode.document->'external_ids'),0)=0
ON CONFLICT(show_entity_id,provider,namespace,normalized_value)
DO UPDATE SET episode_id=EXCLUDED.episode_id;
