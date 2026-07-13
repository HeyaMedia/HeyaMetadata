UPDATE external_id_claims claim
SET state = 'disputed'
WHERE claim.entity_kind = 'artist'
  AND claim.provider = 'musicbrainz'
  AND claim.namespace = 'artist'
  AND claim.state = 'accepted'
  AND NOT EXISTS (
      SELECT 1 FROM normalized_records record
      WHERE record.entity_id = claim.entity_id
        AND record.entity_kind = 'artist'
        AND record.provider = 'musicbrainz'
        AND record.provider_namespace = 'artist'
        AND record.provider_record_id = claim.normalized_value
  )
  AND EXISTS (
      SELECT 1 FROM normalized_records record
      WHERE record.entity_id = claim.entity_id
        AND record.entity_kind = 'artist'
        AND record.provider = 'musicbrainz'
        AND record.provider_namespace = 'artist'
  );
