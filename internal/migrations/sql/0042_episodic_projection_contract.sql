ALTER TABLE image_candidates
    ADD COLUMN owner_resource_id uuid;

ALTER TABLE image_candidates
    DROP CONSTRAINT image_candidates_ownership_scope_check;
ALTER TABLE image_candidates
    ADD CONSTRAINT image_candidates_ownership_scope_check
    CHECK (ownership_scope IN (
        'entity','credit','company','collection','collection_member','recommendation',
        'person_profile','person_credit','season','episode'
    ));

CREATE INDEX image_candidates_resource_owner_idx
    ON image_candidates(owner_resource_id,class)
    WHERE owner_resource_id IS NOT NULL;

CREATE TABLE episodic_season_external_ids (
    season_id uuid NOT NULL REFERENCES episodic_seasons(id) ON DELETE CASCADE,
    show_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    namespace text NOT NULL,
    normalized_value text NOT NULL,
    PRIMARY KEY (season_id, provider, namespace, normalized_value),
    UNIQUE (show_entity_id, provider, namespace, normalized_value)
);

CREATE INDEX episodic_season_external_ids_lookup_idx
    ON episodic_season_external_ids(show_entity_id,provider,namespace,normalized_value);

CREATE TABLE episodic_episode_external_ids (
    episode_id uuid NOT NULL REFERENCES episodic_episodes(id) ON DELETE CASCADE,
    show_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    namespace text NOT NULL,
    normalized_value text NOT NULL,
    PRIMARY KEY (episode_id, provider, namespace, normalized_value),
    UNIQUE (show_entity_id, provider, namespace, normalized_value)
);

CREATE INDEX episodic_episode_external_ids_lookup_idx
    ON episodic_episode_external_ids(show_entity_id,provider,namespace,normalized_value);

INSERT INTO episodic_episode_external_ids(episode_id,show_entity_id,provider,namespace,normalized_value)
SELECT episode.id,episode.show_entity_id,split_part(lower(episode.identity_key),':',1),'episode',lower(episode.document->>'provider_id')
FROM episodic_episodes episode
WHERE COALESCE(episode.document->>'provider_id','') <> ''
  AND split_part(lower(episode.identity_key),':',1) IN ('tmdb','tvdb','tvmaze','anidb')
ON CONFLICT DO NOTHING;
