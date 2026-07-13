package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/jackc/pgx/v5"
)

var (
	ErrCandidateNotFound = errors.New("person reconciliation candidate not found")
	ErrCandidateDecided  = errors.New("person reconciliation candidate is already decided")
)

type ReconciliationCandidate struct {
	LeftPersonID    string          `json:"left_person_id"`
	RightPersonID   string          `json:"right_person_id"`
	LeftName        string          `json:"left_name"`
	RightName       string          `json:"right_name"`
	Score           float64         `json:"score"`
	Reasons         json.RawMessage `json:"reasons"`
	State           string          `json:"state"`
	FirstObservedAt time.Time       `json:"first_observed_at"`
	LastObservedAt  time.Time       `json:"last_observed_at"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
	DecidedBy       string          `json:"decided_by,omitempty"`
	DecisionReason  string          `json:"decision_reason,omitempty"`
	SurvivorID      string          `json:"survivor_person_id,omitempty"`
	AuditLogID      string          `json:"audit_log_id,omitempty"`
}

type ReconciliationDecision struct {
	State      string `json:"state"`
	LeftID     string `json:"left_person_id"`
	RightID    string `json:"right_person_id"`
	SurvivorID string `json:"survivor_person_id,omitempty"`
	RetiredID  string `json:"retired_person_id,omitempty"`
	AuditLogID string `json:"audit_log_id"`
}

func (s *Service) ReconciliationCandidates(ctx context.Context, state string, limit int) ([]ReconciliationCandidate, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		state = "proposed"
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.runtime.DB.Query(ctx, `SELECT candidate.left_person_id::text,candidate.right_person_id::text,left_person.display_name,right_person.display_name,candidate.score,candidate.reasons,candidate.state,candidate.first_observed_at,candidate.last_observed_at,candidate.decided_at,COALESCE(candidate.decided_by,''),COALESCE(candidate.decision_reason,''),COALESCE(candidate.survivor_person_id::text,''),COALESCE(candidate.audit_log_id::text,'') FROM person_reconciliation_candidates candidate JOIN canonical_people left_person ON left_person.entity_id=candidate.left_person_id JOIN canonical_people right_person ON right_person.entity_id=candidate.right_person_id WHERE candidate.state=$1 ORDER BY candidate.score DESC,candidate.last_observed_at DESC LIMIT $2`, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReconciliationCandidate{}
	for rows.Next() {
		var item ReconciliationCandidate
		if err := rows.Scan(&item.LeftPersonID, &item.RightPersonID, &item.LeftName, &item.RightName, &item.Score, &item.Reasons, &item.State, &item.FirstObservedAt, &item.LastObservedAt, &item.DecidedAt, &item.DecidedBy, &item.DecisionReason, &item.SurvivorID, &item.AuditLogID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RejectReconciliation(ctx context.Context, leftID, rightID, actor, reason string) (ReconciliationDecision, error) {
	if err := validateModerationInput(leftID, rightID, actor, reason); err != nil {
		return ReconciliationDecision{}, err
	}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return ReconciliationDecision{}, err
	}
	defer tx.Rollback(ctx)
	candidate, err := lockCandidate(ctx, tx, leftID, rightID)
	if err != nil {
		return ReconciliationDecision{}, err
	}
	if candidate.State == "rejected" {
		return ReconciliationDecision{State: "rejected", LeftID: candidate.LeftPersonID, RightID: candidate.RightPersonID, AuditLogID: candidate.AuditLogID}, nil
	}
	if candidate.State != "proposed" {
		return ReconciliationDecision{}, fmt.Errorf("%w: state is %s", ErrCandidateDecided, candidate.State)
	}
	payload, _ := json.Marshal(map[string]any{"candidate": candidate})
	var auditID string
	if err := tx.QueryRow(ctx, `INSERT INTO moderation_audit_log(entity_kind,action,actor,reason,subject_ids,payload)VALUES('person','person_reconciliation_reject',$1,$2,ARRAY[$3::uuid,$4::uuid],$5)RETURNING id::text`, actor, reason, candidate.LeftPersonID, candidate.RightPersonID, payload).Scan(&auditID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE person_reconciliation_candidates SET state='rejected',decided_at=now(),decided_by=$3,decision_reason=$4,audit_log_id=$5 WHERE left_person_id=$1 AND right_person_id=$2`, candidate.LeftPersonID, candidate.RightPersonID, actor, reason, auditID); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationDecision{}, err
	}
	return ReconciliationDecision{State: "rejected", LeftID: candidate.LeftPersonID, RightID: candidate.RightPersonID, AuditLogID: auditID}, nil
}

func (s *Service) AcceptReconciliation(ctx context.Context, leftID, rightID, survivorID, actor, reason string) (ReconciliationDecision, error) {
	if err := validateModerationInput(leftID, rightID, actor, reason); err != nil {
		return ReconciliationDecision{}, err
	}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return ReconciliationDecision{}, err
	}
	defer tx.Rollback(ctx)
	candidate, err := lockCandidate(ctx, tx, leftID, rightID)
	if err != nil {
		return ReconciliationDecision{}, err
	}
	if survivorID != candidate.LeftPersonID && survivorID != candidate.RightPersonID {
		return ReconciliationDecision{}, fmt.Errorf("survivor must be one of the candidate person IDs")
	}
	if candidate.State == "accepted" && candidate.SurvivorID == survivorID {
		retiredID := candidate.LeftPersonID
		if retiredID == survivorID {
			retiredID = candidate.RightPersonID
		}
		return ReconciliationDecision{State: "accepted", LeftID: candidate.LeftPersonID, RightID: candidate.RightPersonID, SurvivorID: survivorID, RetiredID: retiredID, AuditLogID: candidate.AuditLogID}, nil
	}
	if candidate.State != "proposed" {
		return ReconciliationDecision{}, fmt.Errorf("%w: state is %s", ErrCandidateDecided, candidate.State)
	}
	retiredID := candidate.LeftPersonID
	if retiredID == survivorID {
		retiredID = candidate.RightPersonID
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM entities WHERE id=ANY($1::uuid[]) AND kind='person' AND deleted_at IS NULL`, []string{survivorID, retiredID}).Scan(&activeCount); err != nil {
		return ReconciliationDecision{}, err
	}
	if activeCount != 2 {
		return ReconciliationDecision{}, fmt.Errorf("both candidate people must be active")
	}

	var snapshot []byte
	if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('candidate',to_jsonb(candidate),'people',(SELECT jsonb_agg(to_jsonb(person)) FROM canonical_people person WHERE person.entity_id=ANY($1::uuid[])),'claims',(SELECT jsonb_agg(to_jsonb(claim)) FROM external_id_claims claim WHERE claim.entity_id=ANY($1::uuid[])),'provider_credits',(SELECT jsonb_agg(to_jsonb(credit)) FROM person_provider_credits credit WHERE credit.person_entity_id=ANY($1::uuid[])),'credit_projection_ids',(SELECT jsonb_agg(id) FROM entity_credit_projections WHERE person_entity_id=ANY($1::uuid[])),'search_names',(SELECT jsonb_agg(to_jsonb(name)) FROM search_names name WHERE name.entity_id=ANY($1::uuid[]))) FROM person_reconciliation_candidates candidate WHERE candidate.left_person_id=$2 AND candidate.right_person_id=$3`, []string{survivorID, retiredID}, candidate.LeftPersonID, candidate.RightPersonID).Scan(&snapshot); err != nil {
		return ReconciliationDecision{}, err
	}
	var auditID string
	if err := tx.QueryRow(ctx, `INSERT INTO moderation_audit_log(entity_kind,action,actor,reason,subject_ids,payload)VALUES('person','person_reconciliation_accept',$1,$2,ARRAY[$3::uuid,$4::uuid],$5)RETURNING id::text`, actor, reason, survivorID, retiredID, snapshot).Scan(&auditID); err != nil {
		return ReconciliationDecision{}, err
	}

	if err := mergeCanonicalPeople(ctx, tx, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := mergePersonImages(ctx, tx, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := mergePersonProviderCredits(ctx, tx, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := mergePersonServingState(ctx, tx, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE external_id_claims SET entity_id=$1,last_observed_at=GREATEST(last_observed_at,now()) WHERE entity_id=$2 AND entity_kind='person'`, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE normalized_records SET entity_id=$1 WHERE entity_id=$2 AND entity_kind='person'`, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE entity_credit_projections SET person_entity_id=$1 WHERE person_entity_id=$2`, survivorID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO person_reconciliation_candidates(left_person_id,right_person_id,score,reasons) SELECT LEAST($1::uuid,CASE WHEN left_person_id=$2 THEN right_person_id ELSE left_person_id END),GREATEST($1::uuid,CASE WHEN left_person_id=$2 THEN right_person_id ELSE left_person_id END),score,reasons FROM person_reconciliation_candidates WHERE state='proposed' AND (left_person_id=$2 OR right_person_id=$2) AND NOT (left_person_id=$3 AND right_person_id=$4) ON CONFLICT(left_person_id,right_person_id)DO UPDATE SET score=GREATEST(person_reconciliation_candidates.score,EXCLUDED.score),last_observed_at=now() WHERE person_reconciliation_candidates.state='proposed'`, survivorID, retiredID, candidate.LeftPersonID, candidate.RightPersonID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE person_reconciliation_candidates SET state='superseded',decided_at=now(),decided_by=$2,decision_reason='candidate entity merged into '||$1,audit_log_id=$3 WHERE state='proposed' AND (left_person_id=$4 OR right_person_id=$4)`, survivorID, actor, auditID, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE person_reconciliation_candidates SET state='accepted',decided_at=now(),decided_by=$3,decision_reason=$4,survivor_person_id=$5,audit_log_id=$6 WHERE left_person_id=$1 AND right_person_id=$2`, candidate.LeftPersonID, candidate.RightPersonID, actor, reason, survivorID, auditID); err != nil {
		return ReconciliationDecision{}, err
	}

	var survivorSlug, retiredSlug string
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 RETURNING canonical_version,slug`, survivorID).Scan(&version, &survivorSlug); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := tx.QueryRow(ctx, `UPDATE entities SET deleted_at=now(),updated_at=now() WHERE id=$1 RETURNING slug`, retiredID).Scan(&retiredSlug); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE entity_slugs SET active=false WHERE entity_id=$1`, retiredID); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entity_redirects(retired_entity_id,survivor_entity_id,entity_kind,audit_log_id)VALUES($1,$2,'person',$3)`, retiredID, survivorID, auditID); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := rebuildPersonSearchSummary(ctx, tx, survivorID, version); err != nil {
		return ReconciliationDecision{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version)VALUES($1,'person',$2,'merged',ARRAY['identity','detail','search','credits','artwork'],$3),($4,'person',$5,'redirected',ARRAY['identity'],$3)`, survivorID, survivorSlug, version, retiredID, retiredSlug); err != nil {
		return ReconciliationDecision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationDecision{}, err
	}
	_ = changelog.Sequence(ctx, s.runtime, 100)
	return ReconciliationDecision{State: "accepted", LeftID: candidate.LeftPersonID, RightID: candidate.RightPersonID, SurvivorID: survivorID, RetiredID: retiredID, AuditLogID: auditID}, nil
}

func validateModerationInput(leftID, rightID, actor, reason string) error {
	if strings.TrimSpace(leftID) == "" || strings.TrimSpace(rightID) == "" || leftID == rightID {
		return fmt.Errorf("two distinct candidate person IDs are required")
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("actor and reason are required")
	}
	return nil
}

func lockCandidate(ctx context.Context, tx pgx.Tx, leftID, rightID string) (ReconciliationCandidate, error) {
	var item ReconciliationCandidate
	err := tx.QueryRow(ctx, `SELECT candidate.left_person_id::text,candidate.right_person_id::text,left_person.display_name,right_person.display_name,candidate.score,candidate.reasons,candidate.state,candidate.first_observed_at,candidate.last_observed_at,candidate.decided_at,COALESCE(candidate.decided_by,''),COALESCE(candidate.decision_reason,''),COALESCE(candidate.survivor_person_id::text,''),COALESCE(candidate.audit_log_id::text,'') FROM person_reconciliation_candidates candidate JOIN canonical_people left_person ON left_person.entity_id=candidate.left_person_id JOIN canonical_people right_person ON right_person.entity_id=candidate.right_person_id WHERE candidate.left_person_id=LEAST($1::uuid,$2::uuid) AND candidate.right_person_id=GREATEST($1::uuid,$2::uuid) FOR UPDATE OF candidate`, leftID, rightID).Scan(&item.LeftPersonID, &item.RightPersonID, &item.LeftName, &item.RightName, &item.Score, &item.Reasons, &item.State, &item.FirstObservedAt, &item.LastObservedAt, &item.DecidedAt, &item.DecidedBy, &item.DecisionReason, &item.SurvivorID, &item.AuditLogID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationCandidate{}, ErrCandidateNotFound
	}
	return item, err
}

func mergeCanonicalPeople(ctx context.Context, tx pgx.Tx, survivorID, retiredID string) error {
	_, err := tx.Exec(ctx, `UPDATE canonical_people survivor SET display_name=COALESCE(NULLIF(survivor.display_name,''),retired.display_name),profile_image_id=COALESCE(survivor.profile_image_id,retired.profile_image_id),biography=COALESCE(NULLIF(survivor.biography,''),retired.biography),birth_date=COALESCE(survivor.birth_date,retired.birth_date),death_date=COALESCE(survivor.death_date,retired.death_date),gender=COALESCE(NULLIF(survivor.gender,''),retired.gender),place_of_birth=COALESCE(NULLIF(survivor.place_of_birth,''),retired.place_of_birth),known_for_department=COALESCE(NULLIF(survivor.known_for_department,''),retired.known_for_department),homepage=COALESCE(NULLIF(survivor.homepage,''),retired.homepage),popularity=GREATEST(COALESCE(survivor.popularity,0),COALESCE(retired.popularity,0)),biographies=retired.biographies||survivor.biographies,updated_at=now() FROM canonical_people retired WHERE survivor.entity_id=$1 AND retired.entity_id=$2`, survivorID, retiredID)
	return err
}

func mergePersonImages(ctx context.Context, tx pgx.Tx, survivorID, retiredID string) error {
	if _, err := tx.Exec(ctx, `WITH duplicates AS(SELECT retired.id retired_image_id,survivor.id survivor_image_id FROM image_candidates retired JOIN image_candidates survivor ON survivor.entity_id=$1 AND survivor.provider=retired.provider AND survivor.provider_image_id=retired.provider_image_id AND survivor.class=retired.class WHERE retired.entity_id=$2) UPDATE canonical_people person SET profile_image_id=duplicates.survivor_image_id FROM duplicates WHERE person.entity_id=$1 AND person.profile_image_id=duplicates.retired_image_id`, survivorID, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH duplicates AS(SELECT retired.id retired_image_id,survivor.id survivor_image_id FROM image_candidates retired JOIN image_candidates survivor ON survivor.entity_id=$1 AND survivor.provider=retired.provider AND survivor.provider_image_id=retired.provider_image_id AND survivor.class=retired.class WHERE retired.entity_id=$2) UPDATE person_provider_credits credit SET image_id=duplicates.survivor_image_id FROM duplicates WHERE credit.person_entity_id=ANY($3::uuid[]) AND credit.image_id=duplicates.retired_image_id`, survivorID, retiredID, []string{survivorID, retiredID}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM image_candidates retired USING image_candidates survivor WHERE retired.entity_id=$2 AND survivor.entity_id=$1 AND survivor.provider=retired.provider AND survivor.provider_image_id=retired.provider_image_id AND survivor.class=retired.class`, survivorID, retiredID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE image_candidates SET entity_id=$1 WHERE entity_id=$2`, survivorID, retiredID)
	return err
}

func mergePersonProviderCredits(ctx context.Context, tx pgx.Tx, survivorID, retiredID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO person_provider_credits(person_entity_id,provider,provider_target_id,media_kind,title,release_year,credit_type,character_name,department,job,credit_order,episode_count,image_id,source_observation_id,observed_at) SELECT $1,provider,provider_target_id,media_kind,title,release_year,credit_type,character_name,department,job,credit_order,episode_count,image_id,source_observation_id,observed_at FROM person_provider_credits WHERE person_entity_id=$2 ON CONFLICT(person_entity_id,provider,provider_target_id,credit_type,character_name,department,job)DO UPDATE SET title=CASE WHEN EXCLUDED.observed_at>=person_provider_credits.observed_at THEN EXCLUDED.title ELSE person_provider_credits.title END,release_year=COALESCE(person_provider_credits.release_year,EXCLUDED.release_year),credit_order=LEAST(person_provider_credits.credit_order,EXCLUDED.credit_order),episode_count=GREATEST(person_provider_credits.episode_count,EXCLUDED.episode_count),image_id=COALESCE(person_provider_credits.image_id,EXCLUDED.image_id),source_observation_id=CASE WHEN EXCLUDED.observed_at>=person_provider_credits.observed_at THEN EXCLUDED.source_observation_id ELSE person_provider_credits.source_observation_id END,observed_at=GREATEST(person_provider_credits.observed_at,EXCLUDED.observed_at)`, survivorID, retiredID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM person_provider_credits WHERE person_entity_id=$1`, retiredID)
	return err
}

func mergePersonServingState(ctx context.Context, tx pgx.Tx, survivorID, retiredID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO search_names(entity_id,value,normalized_value,locale,name_type,source_quality)SELECT $1,value,normalized_value,locale,name_type,source_quality FROM search_names WHERE entity_id=$2 ON CONFLICT(entity_id,normalized_value,locale,name_type)DO UPDATE SET value=CASE WHEN EXCLUDED.source_quality>search_names.source_quality THEN EXCLUDED.value ELSE search_names.value END,source_quality=GREATEST(search_names.source_quality,EXCLUDED.source_quality)`, survivorID, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,last_success_at,last_observation_id,failure_class,failure_message,current_job_id,next_eligible_at)SELECT $1,provider,last_attempt_at,last_success_at,last_observation_id,failure_class,failure_message,current_job_id,next_eligible_at FROM provider_refresh_states WHERE entity_id=$2 ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=GREATEST(provider_refresh_states.last_attempt_at,EXCLUDED.last_attempt_at),last_success_at=GREATEST(provider_refresh_states.last_success_at,EXCLUDED.last_success_at),last_observation_id=COALESCE(EXCLUDED.last_observation_id,provider_refresh_states.last_observation_id),failure_class=COALESCE(provider_refresh_states.failure_class,EXCLUDED.failure_class),failure_message=COALESCE(provider_refresh_states.failure_message,EXCLUDED.failure_message),current_job_id=COALESCE(provider_refresh_states.current_job_id,EXCLUDED.current_job_id),next_eligible_at=GREATEST(provider_refresh_states.next_eligible_at,EXCLUDED.next_eligible_at)`, survivorID, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM provider_refresh_states WHERE entity_id=$1`, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entity_access_stats(entity_id,total_fetches,decayed_score,last_accessed_at,score_updated_at,updated_at)SELECT $1,total_fetches,decayed_score,last_accessed_at,score_updated_at,now() FROM entity_access_stats WHERE entity_id=$2 ON CONFLICT(entity_id)DO UPDATE SET total_fetches=entity_access_stats.total_fetches+EXCLUDED.total_fetches,decayed_score=entity_access_stats.decayed_score+EXCLUDED.decayed_score,last_accessed_at=GREATEST(entity_access_stats.last_accessed_at,EXCLUDED.last_accessed_at),score_updated_at=GREATEST(entity_access_stats.score_updated_at,EXCLUDED.score_updated_at),updated_at=now()`, survivorID, retiredID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_access_stats WHERE entity_id=$1`, retiredID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM search_entities WHERE entity_id=$1`, retiredID)
	return err
}

func rebuildPersonSearchSummary(ctx context.Context, tx pgx.Tx, entityID string, version int64) error {
	var displayName, imageID, slug string
	if err := tx.QueryRow(ctx, `SELECT person.display_name,COALESCE(person.profile_image_id::text,''),entity.slug FROM canonical_people person JOIN entities entity ON entity.id=person.entity_id WHERE person.entity_id=$1`, entityID).Scan(&displayName, &imageID, &slug); err != nil {
		return err
	}
	summary, _ := json.Marshal(map[string]any{"schema_version": 1, "projection_version": version, "id": entityID, "kind": "person", "slug": slug, "display": map[string]any{"title": displayName, "image_id": imageID}})
	_, err := tx.Exec(ctx, `UPDATE search_entities SET display_title=$2,summary=$3,projection_version=$4,updated_at=now() WHERE entity_id=$1`, entityID, displayName, summary, version)
	return err
}
