package people

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func persistPersonExternalEvidence(ctx context.Context, tx pgx.Tx, entityID, sourceProvider, observationID, normalizedRecordID string, observedAt time.Time, claims []PersonExternalID) error {
	for _, raw := range claims {
		claim := normalizePersonExternalID(raw)
		if claim.Provider == "" || claim.Namespace == "" || claim.Value == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO person_identity_evidence(person_entity_id,provider,namespace,normalized_value,source_provider,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,$7)ON CONFLICT(person_entity_id,provider,namespace,normalized_value,source_provider)DO UPDATE SET source_observation_id=COALESCE(EXCLUDED.source_observation_id,person_identity_evidence.source_observation_id),last_observed_at=GREATEST(person_identity_evidence.last_observed_at,EXCLUDED.last_observed_at)`, entityID, claim.Provider, claim.Namespace, claim.Value, sourceProvider, observationID, observedAt); err != nil {
			return err
		}

		var owner string
		err := tx.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='person' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, claim.Provider, claim.Namespace, claim.Value).Scan(&owner)
		if err == nil && owner != entityID {
			conflictBody, _ := json.Marshal([]map[string]string{{"entity_id": owner, "provider": claim.Provider, "namespace": claim.Namespace, "value": claim.Value}, {"entity_id": entityID, "provider": claim.Provider, "namespace": claim.Namespace, "value": claim.Value}})
			if _, insertErr := tx.Exec(ctx, `INSERT INTO external_id_conflicts(entity_kind,claims,normalized_record_id)SELECT 'person',$1,NULLIF($2,'')::uuid WHERE NOT EXISTS(SELECT 1 FROM external_id_conflicts WHERE entity_kind='person' AND state='open' AND claims=$1)`, conflictBody, normalizedRecordID); insertErr != nil {
				return insertErr
			}
			continue
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'person',$2,$3,$4,'accepted',1,NULLIF($5,'')::uuid,$6,$6)ON CONFLICT(entity_kind,provider,namespace,normalized_value)DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,source_observation_id=EXCLUDED.source_observation_id WHERE external_id_claims.entity_id=EXCLUDED.entity_id`, entityID, claim.Provider, claim.Namespace, claim.Value, observationID, observedAt); err != nil {
			return err
		}
	}
	return nil
}

func normalizePersonExternalID(value PersonExternalID) PersonExternalID {
	value.Provider = strings.ToLower(strings.TrimSpace(value.Provider))
	value.Namespace = strings.ToLower(strings.TrimSpace(value.Namespace))
	value.Value = strings.TrimSpace(value.Value)
	switch value.Provider {
	case "imdb":
		value.Value = strings.ToLower(value.Value)
	case "wikidata":
		value.Value = strings.ToUpper(value.Value)
	}
	return value
}
