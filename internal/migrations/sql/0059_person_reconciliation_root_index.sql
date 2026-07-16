CREATE INDEX external_id_claims_person_reconciliation_roots_idx
    ON external_id_claims(entity_id,provider)
    WHERE entity_kind='person'
      AND namespace='person'
      AND provider IN('tmdb','tvmaze','tvdb')
      AND state='accepted';
