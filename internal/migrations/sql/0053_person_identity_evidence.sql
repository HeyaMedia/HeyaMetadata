CREATE TABLE person_identity_evidence (
    person_entity_id uuid NOT NULL REFERENCES canonical_people(entity_id) ON DELETE CASCADE,
    provider text NOT NULL,
    namespace text NOT NULL,
    normalized_value text NOT NULL,
    source_provider text NOT NULL,
    source_observation_id uuid REFERENCES provider_observations(id),
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (person_entity_id,provider,namespace,normalized_value,source_provider)
);

CREATE INDEX person_identity_evidence_value_idx
    ON person_identity_evidence(provider,namespace,normalized_value,person_entity_id);

-- Preserve the evidence behind existing person claims. The accepted claim
-- remains the unique owner; this table records which upstream supplied it so
-- another provider can independently corroborate the same human identity.
INSERT INTO person_identity_evidence(
    person_entity_id,provider,namespace,normalized_value,source_provider,
    source_observation_id,first_observed_at,last_observed_at
)
SELECT claim.entity_id,claim.provider,claim.namespace,claim.normalized_value,
       COALESCE(observation.provider,claim.provider),claim.source_observation_id,
       claim.first_observed_at,claim.last_observed_at
FROM external_id_claims claim
LEFT JOIN provider_observations observation ON observation.id=claim.source_observation_id
WHERE claim.entity_kind='person' AND claim.state='accepted'
  AND claim.provider IN('imdb','wikidata','facebook','instagram','twitter','youtube','tiktok')
ON CONFLICT DO NOTHING;
