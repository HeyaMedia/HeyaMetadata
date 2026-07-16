package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type reconciliationEvidence struct {
	otherID                                         string
	sameBirth, sameDeath, samePlace, sharedExternal bool
	canonicalOverlap, providerOverlap               int
	mineRootProviders, otherRootProviders           int
	sameRootProvider                                bool
}

type verifiedReconciliation struct {
	leftID, rightID, reason string
}

// Reconcile evaluates one active person against exact-primary-name peers.
// Names select candidates but never authorize a merge: automatic acceptance
// requires independent external-ID, biographical, filmography, or established
// multi-provider consensus evidence.
func (s *Service) Reconcile(ctx context.Context, entityID string) error {
	return s.proposeReconciliations(ctx, entityID)
}

func (s *Service) proposeReconciliations(ctx context.Context, entityID string) error {
	rows, err := s.runtime.DB.Query(ctx, `
		SELECT other.entity_id::text,
		       COALESCE(mine.birth_date IS NOT NULL AND mine.birth_date=other.birth_date,false),
		       COALESCE(mine.death_date IS NOT NULL AND mine.death_date=other.death_date,false),
		       COALESCE(mine.place_of_birth IS NOT NULL AND other.place_of_birth IS NOT NULL AND lower(unaccent(mine.place_of_birth))=lower(unaccent(other.place_of_birth)),false),
		       (SELECT count(DISTINCT a.entity_id)::int FROM entity_credit_projections a JOIN entity_credit_projections b ON b.entity_id=a.entity_id WHERE a.person_entity_id=mine.entity_id AND b.person_entity_id=other.entity_id),
		       (SELECT count(*)::int FROM (SELECT lower(unaccent(a.title)) AS title,COALESCE(a.release_year,0) AS release_year FROM person_provider_credits a JOIN person_provider_credits b ON lower(unaccent(b.title))=lower(unaccent(a.title)) AND COALESCE(b.release_year,0)=COALESCE(a.release_year,0) WHERE a.person_entity_id=mine.entity_id AND b.person_entity_id=other.entity_id AND a.provider<>b.provider GROUP BY lower(unaccent(a.title)),COALESCE(a.release_year,0)) overlap),
		       EXISTS(SELECT 1 FROM person_identity_evidence a JOIN person_identity_evidence b ON b.provider=a.provider AND b.namespace=a.namespace AND b.normalized_value=a.normalized_value WHERE a.person_entity_id=mine.entity_id AND b.person_entity_id=other.entity_id AND a.provider IN('imdb','wikidata') AND a.source_provider<>b.source_provider),
		       (SELECT count(DISTINCT provider)::int FROM external_id_claims WHERE entity_id=mine.entity_id AND entity_kind='person' AND namespace='person' AND provider IN('tmdb','tvmaze','tvdb') AND state='accepted'),
		       (SELECT count(DISTINCT provider)::int FROM external_id_claims WHERE entity_id=other.entity_id AND entity_kind='person' AND namespace='person' AND provider IN('tmdb','tvmaze','tvdb') AND state='accepted'),
		       EXISTS(SELECT 1 FROM external_id_claims a JOIN external_id_claims b ON b.provider=a.provider WHERE a.entity_id=mine.entity_id AND b.entity_id=other.entity_id AND a.entity_kind='person' AND b.entity_kind='person' AND a.namespace='person' AND b.namespace='person' AND a.provider IN('tmdb','tvmaze','tvdb') AND a.state='accepted' AND b.state='accepted')
		FROM canonical_people mine
		JOIN canonical_people other ON other.normalized_display_name=mine.normalized_display_name AND other.entity_id<>mine.entity_id
		JOIN entities other_entity ON other_entity.id=other.entity_id AND other_entity.deleted_at IS NULL
		WHERE mine.entity_id=$1`, entityID)
	if err != nil {
		return fmt.Errorf("find person reconciliation peers: %w", err)
	}
	var evidence []reconciliationEvidence
	for rows.Next() {
		var item reconciliationEvidence
		if err := rows.Scan(&item.otherID, &item.sameBirth, &item.sameDeath, &item.samePlace, &item.canonicalOverlap, &item.providerOverlap, &item.sharedExternal, &item.mineRootProviders, &item.otherRootProviders, &item.sameRootProvider); err != nil {
			rows.Close()
			return fmt.Errorf("scan person reconciliation evidence: %w", err)
		}
		evidence = append(evidence, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate person reconciliation evidence: %w", err)
	}
	rows.Close()

	var verified []verifiedReconciliation
	for _, item := range evidence {
		score, reasons, automaticReason := assessReconciliation(item)
		if score < 0.80 {
			continue
		}
		left, right := entityID, item.otherID
		if strings.Compare(left, right) > 0 {
			left, right = right, left
		}
		body, _ := json.Marshal(reasons)
		var state string
		err := s.runtime.DB.QueryRow(ctx, `INSERT INTO person_reconciliation_candidates(left_person_id,right_person_id,score,reasons)VALUES($1,$2,$3,$4)ON CONFLICT(left_person_id,right_person_id)DO UPDATE SET score=EXCLUDED.score,reasons=EXCLUDED.reasons,last_observed_at=now() WHERE person_reconciliation_candidates.state='proposed' RETURNING state`, left, right, score, body).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("record person reconciliation candidate: %w", err)
		}
		if state == "proposed" && automaticReason != "" {
			verified = append(verified, verifiedReconciliation{leftID: left, rightID: right, reason: automaticReason})
		}
	}
	for _, candidate := range verified {
		if err := s.acceptVerifiedReconciliation(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
}

func assessReconciliation(value reconciliationEvidence) (float64, []string, string) {
	score := 0.10
	reasons := []string{"exact_primary_name"}
	if value.sharedExternal {
		score += 0.90
		reasons = append(reasons, "shared_external_person_id")
	}
	if value.sameBirth {
		score += 0.50
		reasons = append(reasons, "exact_birth_date")
	}
	if value.sameDeath {
		score += 0.10
		reasons = append(reasons, "exact_death_date")
	}
	if value.samePlace {
		score += 0.10
		reasons = append(reasons, "exact_birth_place")
	}
	if value.canonicalOverlap > 0 {
		score += min(float64(value.canonicalOverlap)*0.15, 0.30)
		reasons = append(reasons, fmt.Sprintf("shared_canonical_credits:%d", value.canonicalOverlap))
	}
	if value.providerOverlap > 0 {
		score += min(float64(value.providerOverlap)*0.10, 0.30)
		reasons = append(reasons, fmt.Sprintf("cross_provider_filmography:%d", value.providerOverlap))
	}
	maxRoots := max(value.mineRootProviders, value.otherRootProviders)
	if maxRoots >= 2 && value.canonicalOverlap > 0 && value.providerOverlap > 0 {
		score += 0.50
		reasons = append(reasons, "established_provider_consensus")
	}
	if value.canonicalOverlap > 0 && value.providerOverlap >= 2 {
		score += 0.50
		reasons = append(reasons, "multiple_filmography_matches")
	}
	if score > 1 {
		score = 1
	}
	if value.sameRootProvider {
		return score, append(reasons, "same_provider_roots_require_review"), ""
	}
	switch {
	case value.sharedExternal:
		return score, reasons, "independent upstreams supplied the same external person identifier"
	case value.sameBirth && value.canonicalOverlap > 0 && value.providerOverlap > 0:
		return score, reasons, "exact birth date and cross-provider filmography converge"
	case value.canonicalOverlap > 0 && value.providerOverlap >= 2:
		return score, reasons, "multiple cross-provider filmography credits converge"
	case maxRoots >= 2 && value.canonicalOverlap > 0 && value.providerOverlap > 0:
		return score, reasons, "two verified provider roots corroborate the additional identity"
	default:
		return score, reasons, ""
	}
}

func (s *Service) acceptVerifiedReconciliation(ctx context.Context, candidate verifiedReconciliation) error {
	left, leftErr := s.CanonicalID(ctx, candidate.leftID)
	right, rightErr := s.CanonicalID(ctx, candidate.rightID)
	if leftErr != nil || rightErr != nil {
		if errors.Is(leftErr, ErrNotFound) || errors.Is(rightErr, ErrNotFound) {
			return nil
		}
		return errors.Join(leftErr, rightErr)
	}
	if left == right {
		return nil
	}
	survivor, err := s.preferredPersonSurvivor(ctx, left, right)
	if err != nil {
		return err
	}
	_, err = s.AcceptReconciliation(ctx, left, right, survivor, "system:person-reconciler", candidate.reason)
	if errors.Is(err, ErrCandidateDecided) || errors.Is(err, ErrCandidateNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("accept verified person reconciliation: %w", err)
	}
	return nil
}

func (s *Service) preferredPersonSurvivor(ctx context.Context, leftID, rightID string) (string, error) {
	var survivor string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity.id::text FROM entities entity WHERE entity.id=ANY($1::uuid[]) AND entity.deleted_at IS NULL ORDER BY EXISTS(SELECT 1 FROM external_id_claims claim WHERE claim.entity_id=entity.id AND claim.entity_kind='person' AND claim.provider='tmdb' AND claim.namespace='person' AND claim.state='accepted') DESC,(SELECT count(DISTINCT provider) FROM external_id_claims claim WHERE claim.entity_id=entity.id AND claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted') DESC,(SELECT count(*) FROM entity_credit_projections credit WHERE credit.person_entity_id=entity.id) DESC,entity.created_at,entity.id LIMIT 1`, []string{leftID, rightID}).Scan(&survivor)
	if err != nil {
		return "", fmt.Errorf("choose canonical person survivor: %w", err)
	}
	return survivor, nil
}

// ReconciliationRoots returns a bounded set of exact-primary-name people from
// different providers. The background scheduler enriches these roots so the
// evidence evaluator can distinguish true cross-provider identities from
// namesakes without fanning out over every credited person.
func (s *Service) ReconciliationRoots(ctx context.Context, limit int) (map[string]map[string]string, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	// Bound the expensive identity and access-score evaluation to the stalest
	// roots. Successful and failed provider attempts both advance freshness, so
	// later roots move through this pool instead of requiring a full people scan
	// on every scheduler tick.
	rootPoolLimit := max(2000, limit*50)
	candidatePoolLimit := max(500, limit*5)
	rows, err := s.runtime.DB.Query(ctx, `WITH root_people AS MATERIALIZED (
		SELECT claim.entity_id,
		       min(COALESCE(refresh.last_attempt_at,'-infinity'::timestamptz)) AS evidence_attempt,
		       array_agg(DISTINCT claim.provider) AS providers
		FROM external_id_claims claim
		LEFT JOIN provider_refresh_states refresh ON refresh.entity_id=claim.entity_id AND refresh.provider=claim.provider
		WHERE claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted'
		GROUP BY claim.entity_id
		ORDER BY evidence_attempt,claim.entity_id
		LIMIT $2
	), candidate_people AS MATERIALIZED (
		SELECT mine_root.entity_id,mine_root.evidence_attempt
		FROM root_people mine_root
		JOIN canonical_people mine ON mine.entity_id=mine_root.entity_id
		JOIN canonical_people other ON other.normalized_display_name=mine.normalized_display_name AND other.entity_id<>mine.entity_id
		JOIN entities mine_entity ON mine_entity.id=mine.entity_id AND mine_entity.deleted_at IS NULL
		JOIN entities other_entity ON other_entity.id=other.entity_id AND other_entity.deleted_at IS NULL
		WHERE EXISTS(
			SELECT 1 FROM external_id_claims other_claim
			WHERE other_claim.entity_id=other.entity_id AND other_claim.entity_kind='person' AND other_claim.namespace='person'
			  AND other_claim.provider IN('tmdb','tvmaze','tvdb') AND other_claim.state='accepted'
			  AND (cardinality(mine_root.providers)>1 OR other_claim.provider<>mine_root.providers[1])
		)
		GROUP BY mine_root.entity_id,mine_root.evidence_attempt
		ORDER BY mine_root.evidence_attempt,mine_root.entity_id
		LIMIT $3
	), ranked AS (
		SELECT candidate.entity_id,candidate.evidence_attempt,COALESCE(max(stats.decayed_score),0) AS access_score
		FROM candidate_people candidate
		LEFT JOIN entity_credit_projections credit ON credit.person_entity_id=candidate.entity_id
		LEFT JOIN entity_access_stats stats ON stats.entity_id=credit.entity_id
		GROUP BY candidate.entity_id,candidate.evidence_attempt
		ORDER BY candidate.evidence_attempt,access_score DESC,candidate.entity_id
		LIMIT $1
	)
	SELECT claim.entity_id::text,claim.provider,claim.normalized_value
	FROM ranked
	JOIN external_id_claims claim ON claim.entity_id=ranked.entity_id
	WHERE claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted'
	ORDER BY claim.entity_id,claim.provider`, limit, rootPoolLimit, candidatePoolLimit)
	if err != nil {
		return nil, fmt.Errorf("select person reconciliation roots: %w", err)
	}
	defer rows.Close()
	result := map[string]map[string]string{}
	for rows.Next() {
		var entityID, provider, value string
		if err := rows.Scan(&entityID, &provider, &value); err != nil {
			return nil, err
		}
		if result[entityID] == nil {
			result[entityID] = map[string]string{}
		}
		result[entityID][provider] = value
	}
	return result, rows.Err()
}
