-- request_key can contain structured discovery evidence and exceed PostgreSQL's
-- B-tree tuple limit. Keep the exact value as provenance, but bound the unique
-- identity index by hashing the two unbounded provider-controlled components.
ALTER TABLE provider_observations
    DROP CONSTRAINT IF EXISTS provider_observations_provider_provider_namespace_provider__key;

CREATE UNIQUE INDEX provider_observations_identity_time_uidx
    ON provider_observations (
        provider,
        provider_namespace,
        md5(provider_record_id),
        md5(request_key),
        observed_at
    );
