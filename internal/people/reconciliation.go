package people

import (
	"context"
	"encoding/json"
	"strings"
)

// proposeReconciliations records reviewable identity candidates. It never
// mutates claims or credits: exact names merely select candidates, while a
// proposal requires independent birth/biographical or filmography evidence.
func (s *Service) proposeReconciliations(ctx context.Context, entityID string) error {
	rows, err := s.runtime.DB.Query(ctx, `
		SELECT DISTINCT other.entity_id::text,
		       COALESCE(mine.birth_date IS NOT NULL AND mine.birth_date=other.birth_date,false) AS same_birth,
		       COALESCE(mine.death_date IS NOT NULL AND mine.death_date=other.death_date,false) AS same_death,
		       COALESCE(mine.place_of_birth IS NOT NULL AND other.place_of_birth IS NOT NULL AND lower(mine.place_of_birth)=lower(other.place_of_birth),false) AS same_place,
		       EXISTS(SELECT 1 FROM entity_credit_projections a JOIN entity_credit_projections b ON b.entity_id=a.entity_id WHERE a.person_entity_id=$1 AND b.person_entity_id=other.entity_id) AS canonical_overlap,
		       EXISTS(SELECT 1 FROM person_provider_credits a JOIN person_provider_credits b ON lower(b.title)=lower(a.title) AND COALESCE(b.release_year,0)=COALESCE(a.release_year,0) WHERE a.person_entity_id=$1 AND b.person_entity_id=other.entity_id AND a.provider<>b.provider) AS provider_overlap
		FROM canonical_people mine
		JOIN search_names mine_name ON mine_name.entity_id=mine.entity_id
		JOIN search_names other_name ON other_name.normalized_value=mine_name.normalized_value AND other_name.entity_id<>mine.entity_id
		JOIN canonical_people other ON other.entity_id=other_name.entity_id
		JOIN entities other_entity ON other_entity.id=other.entity_id AND other_entity.deleted_at IS NULL
		WHERE mine.entity_id=$1`, entityID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var otherID string
		var sameBirth, sameDeath, samePlace, canonicalOverlap, providerOverlap bool
		if err := rows.Scan(&otherID, &sameBirth, &sameDeath, &samePlace, &canonicalOverlap, &providerOverlap); err != nil {
			return err
		}
		score := 0.10
		reasons := []string{"exact_normalized_name"}
		if sameBirth {
			score += 0.50
			reasons = append(reasons, "exact_birth_date")
		}
		if sameDeath {
			score += 0.10
			reasons = append(reasons, "exact_death_date")
		}
		if samePlace {
			score += 0.10
			reasons = append(reasons, "exact_birth_place")
		}
		if canonicalOverlap {
			score += 0.30
			reasons = append(reasons, "shared_canonical_credit")
		}
		if providerOverlap {
			score += 0.25
			reasons = append(reasons, "cross_provider_filmography_overlap")
		}
		if score < 0.80 {
			continue
		}
		if score > 1 {
			score = 1
		}
		left, right := entityID, otherID
		if strings.Compare(left, right) > 0 {
			left, right = right, left
		}
		body, _ := json.Marshal(reasons)
		if _, err := s.runtime.DB.Exec(ctx, `INSERT INTO person_reconciliation_candidates(left_person_id,right_person_id,score,reasons)VALUES($1,$2,$3,$4)ON CONFLICT(left_person_id,right_person_id)DO UPDATE SET score=EXCLUDED.score,reasons=EXCLUDED.reasons,last_observed_at=now() WHERE person_reconciliation_candidates.state='proposed'`, left, right, score, body); err != nil {
			return err
		}
	}
	return rows.Err()
}
