ALTER TABLE provider_observations
    ADD COLUMN reusable_until timestamptz;

CREATE INDEX provider_observations_reuse_idx
    ON provider_observations (provider, request_fingerprint, reusable_until DESC)
    WHERE reusable_until IS NOT NULL;
