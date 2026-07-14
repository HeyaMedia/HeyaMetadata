CREATE TABLE episodic_series_external_evidence (
    entity_kind text NOT NULL,
    anchor_provider text NOT NULL,
    anchor_namespace text NOT NULL,
    anchor_value text NOT NULL,
    provider text NOT NULL,
    namespace text NOT NULL,
    normalized_value text NOT NULL,
    source_observation_id uuid REFERENCES provider_observations(id) ON DELETE SET NULL,
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    PRIMARY KEY (
        entity_kind,
        anchor_provider,
        anchor_namespace,
        anchor_value,
        provider,
        namespace,
        normalized_value
    )
);

CREATE INDEX episodic_series_external_evidence_lookup_idx
    ON episodic_series_external_evidence (
        entity_kind,
        anchor_provider,
        anchor_namespace,
        anchor_value
    );

-- AniDB models Attack on Titan seasons as separate anime while IMDb, TMDB,
-- and TVDB identify the broader series. Preserve the IMDb relationship as
-- series-level evidence and bind its accepted identity to the season-one root.
INSERT INTO episodic_series_external_evidence (
    entity_kind,
    anchor_provider,
    anchor_namespace,
    anchor_value,
    provider,
    namespace,
    normalized_value,
    source_observation_id,
    first_observed_at,
    last_observed_at
)
SELECT
    'anime',
    'tvdb',
    'series',
    '267440',
    'imdb',
    'title',
    'tt2560140',
    claim.source_observation_id,
    claim.first_observed_at,
    claim.last_observed_at
FROM external_id_claims claim
WHERE claim.entity_kind = 'anime'
  AND claim.provider = 'imdb'
  AND claim.namespace = 'title'
  AND claim.normalized_value = 'tt2560140'
ON CONFLICT DO NOTHING;

WITH root AS (
    SELECT entity_id
    FROM external_id_claims
    WHERE entity_kind = 'anime'
      AND provider = 'anidb'
      AND namespace = 'anime'
      AND normalized_value = '9541'
      AND state = 'accepted'
)
UPDATE external_id_claims claim
SET entity_id = root.entity_id,
    state = 'accepted',
    confidence = 1,
    source_observation_id = NULL,
    last_observed_at = now()
FROM root
WHERE claim.entity_kind = 'anime'
  AND claim.provider = 'imdb'
  AND claim.namespace = 'title'
  AND claim.normalized_value = 'tt2560140';
