INSERT INTO external_id_claims (
    entity_id, entity_kind, provider, namespace, normalized_value,
    state, confidence, first_observed_at, last_observed_at
)
SELECT DISTINCT
    track.recording_entity_id, 'recording', source->>'provider', 'track',
    lower(source->>'provider_id'), 'accepted', 1, now(), now()
FROM release_tracks track
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(track.document->'sources')='array'
         THEN track.document->'sources' ELSE '[]'::jsonb END
) source
WHERE track.recording_entity_id IS NOT NULL
  AND COALESCE(source->>'provider','') <> ''
  AND COALESCE(source->>'provider_id','') <> ''
  AND source->>'provider' <> 'musicbrainz'
ON CONFLICT (entity_kind, provider, namespace, normalized_value) DO NOTHING;
