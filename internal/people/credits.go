package people

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// CanonicalCredit is the provider-independent credit row embedded in movie,
// television, and anime documents. Provider fields remain passive provenance;
// PersonEntityID is the only identity clients follow.
type CanonicalCredit struct {
	PersonEntityID   string `json:"person_entity_id"`
	Provider         string `json:"provider"`
	ProviderPersonID string `json:"provider_person_id"`
	DisplayName      string `json:"display_name"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character,omitempty"`
	Department       string `json:"department,omitempty"`
	Job              string `json:"job,omitempty"`
	Order            int    `json:"order,omitempty"`
	ProfileImageID   string `json:"profile_image_id,omitempty"`
}

// CreditIdentity is the provider identity used by the projection trigger to
// find or create a canonical person.
type CreditIdentity struct {
	Provider         string
	ProviderPersonID string
}

// LockCreditPersonCanonicalization acquires transaction-scoped locks for the
// complete credit identity set in deterministic canonical order. Concurrent
// projections that share people are serialized, while disjoint casts remain
// fully parallel.
func LockCreditPersonCanonicalization(ctx context.Context, tx pgx.Tx, identities []CreditIdentity) error {
	if len(identities) == 0 {
		return nil
	}
	providers := make([]string, 0, len(identities))
	personIDs := make([]string, 0, len(identities))
	for _, identity := range identities {
		if strings.TrimSpace(identity.Provider) == "" || strings.TrimSpace(identity.ProviderPersonID) == "" {
			continue
		}
		providers = append(providers, identity.Provider)
		personIDs = append(personIDs, identity.ProviderPersonID)
	}
	if len(providers) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT heya_lock_credit_people($1::text[],$2::text[])`, providers, personIDs); err != nil {
		return fmt.Errorf("lock credit people for canonicalization: %w", err)
	}
	return nil
}

type storedCredit struct {
	id int64
	CanonicalCredit
}

// CanonicalizeEntityCredits collapses equivalent observations only after they
// resolve to the same canonical person. Same-name people with different Heya
// IDs can therefore never be combined by this path.
func CanonicalizeEntityCredits(ctx context.Context, tx pgx.Tx, entityID string, projectionVersion int64) ([]CanonicalCredit, error) {
	rows, err := tx.Query(ctx, `SELECT id,person_entity_id::text,provider,provider_person_id,display_name,credit_type,COALESCE(character_name,''),COALESCE(department,''),COALESCE(job,''),credit_order,COALESCE(profile_image_id::text,'') FROM entity_credit_projections WHERE entity_id=$1 ORDER BY CASE credit_type WHEN 'cast' THEN 0 ELSE 1 END,CASE provider WHEN 'tmdb' THEN 0 WHEN 'tvmaze' THEN 1 WHEN 'tvdb' THEN 2 ELSE 10 END,credit_order,id`, entityID)
	if err != nil {
		return nil, fmt.Errorf("read entity credits for canonicalization: %w", err)
	}
	defer rows.Close()
	var values []storedCredit
	for rows.Next() {
		var value storedCredit
		if err := rows.Scan(&value.id, &value.PersonEntityID, &value.Provider, &value.ProviderPersonID, &value.DisplayName, &value.CreditType, &value.Character, &value.Department, &value.Job, &value.Order, &value.ProfileImageID); err != nil {
			return nil, fmt.Errorf("scan entity credit for canonicalization: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity credits for canonicalization: %w", err)
	}

	kept := make([]storedCredit, 0, len(values))
	duplicateIDs := make([]int64, 0)
	specificCastRoles := countSpecificCastRoles(values)
	for _, candidate := range values {
		duplicate := false
		for _, existing := range kept {
			if equivalentCredit(existing.CanonicalCredit, candidate.CanonicalCredit, specificCastRoles[candidate.PersonEntityID]) {
				duplicate = true
				break
			}
		}
		if duplicate {
			duplicateIDs = append(duplicateIDs, candidate.id)
			continue
		}
		kept = append(kept, candidate)
	}
	if len(duplicateIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM entity_credit_projections WHERE id=ANY($1::bigint[])`, duplicateIDs); err != nil {
			return nil, fmt.Errorf("remove equivalent entity credits: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE entity_credit_projections SET projection_version=$2 WHERE entity_id=$1`, entityID, projectionVersion); err != nil {
		return nil, fmt.Errorf("version canonical entity credits: %w", err)
	}
	result := make([]CanonicalCredit, 0, len(kept))
	for _, value := range kept {
		result = append(result, value.CanonicalCredit)
	}
	return result, nil
}

func equivalentCredit(left, right CanonicalCredit, specificCastRoleCount int) bool {
	if left.PersonEntityID == "" || left.PersonEntityID != right.PersonEntityID || left.CreditType != right.CreditType {
		return false
	}
	leftCharacter, rightCharacter := normalizedRole(left.Character), normalizedRole(right.Character)
	leftDepartment, rightDepartment := normalizedRole(left.Department), normalizedRole(right.Department)
	leftJob, rightJob := normalizedRole(left.Job), normalizedRole(right.Job)
	if left.Provider == right.Provider {
		return leftCharacter == rightCharacter && leftDepartment == rightDepartment && leftJob == rightJob
	}
	if left.CreditType == "cast" {
		leftRole := semanticCastRole(leftCharacter, normalizedRole(left.DisplayName))
		rightRole := semanticCastRole(rightCharacter, normalizedRole(right.DisplayName))
		return compatibleCastRole(leftRole, rightRole, specificCastRoleCount)
	}
	if leftJob == "" || rightJob == "" || leftJob != rightJob {
		return false
	}
	if leftDepartment != "" && rightDepartment != "" && leftDepartment != rightDepartment {
		return false
	}
	return leftCharacter == rightCharacter || leftCharacter == "" || rightCharacter == ""
}

func semanticCastRole(character, displayName string) string {
	if character == "" || character == displayName || character == "self" || character == "himself" || character == "herself" || character == "themselves" {
		return "self"
	}
	for _, prefix := range []string{"self ", "himself ", "herself ", "themselves "} {
		if strings.HasPrefix(character, prefix) {
			if role := strings.TrimSpace(strings.TrimPrefix(character, prefix)); role != "" {
				return role
			}
			return "self"
		}
	}
	return character
}

func compatibleCastRole(left, right string, specificRoleCount int) bool {
	if left == right {
		return true
	}
	if left == "self" || right == "self" {
		return specificRoleCount <= 1
	}
	return roleContains(left, right) || roleContains(right, left)
}

func countSpecificCastRoles(values []storedCredit) map[string]int {
	roles := map[string]map[string]struct{}{}
	for _, value := range values {
		if value.CreditType != "cast" || value.PersonEntityID == "" {
			continue
		}
		role := semanticCastRole(normalizedRole(value.Character), normalizedRole(value.DisplayName))
		if role == "" || role == "self" {
			continue
		}
		if roles[value.PersonEntityID] == nil {
			roles[value.PersonEntityID] = map[string]struct{}{}
		}
		roles[value.PersonEntityID][role] = struct{}{}
	}
	result := make(map[string]int, len(roles))
	for personID, personRoles := range roles {
		result[personID] = len(personRoles)
	}
	return result
}

func roleContains(value, candidate string) bool {
	if value == "" || candidate == "" {
		return false
	}
	return strings.Contains(" "+value+" ", " "+candidate+" ")
}

func normalizedRole(value string) string {
	var result strings.Builder
	space := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			if space && result.Len() > 0 {
				result.WriteByte(' ')
			}
			space = false
			result.WriteRune(character)
		} else {
			space = true
		}
	}
	return strings.TrimSpace(result.String())
}

func rebuildAffectedCreditDocuments(ctx context.Context, tx pgx.Tx, entityIDs []string) error {
	slices.Sort(entityIDs)
	entityIDs = slices.Compact(entityIDs)
	for _, entityID := range entityIDs {
		var kind, slug string
		var version int64
		if err := tx.QueryRow(ctx, `UPDATE entities SET canonical_version=canonical_version+1,updated_at=now() WHERE id=$1 AND deleted_at IS NULL RETURNING kind,slug,canonical_version`, entityID).Scan(&kind, &slug, &version); err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return fmt.Errorf("version credit-dependent entity %s: %w", entityID, err)
		}
		credits, err := CanonicalizeEntityCredits(ctx, tx, entityID, version)
		if err != nil {
			return err
		}
		preview := credits
		if len(preview) > 50 {
			preview = preview[:50]
		}
		creditBody, err := json.Marshal(preview)
		if err != nil {
			return fmt.Errorf("encode canonical credits: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE api_documents SET projection_version=$2,document=jsonb_set(CASE WHEN document_kind='detail' THEN jsonb_set(document,'{data,credits}',$3::jsonb,true) ELSE document END,'{projection_version}',to_jsonb($2::bigint),true),updated_at=now() WHERE entity_id=$1`, entityID, version, creditBody); err != nil {
			return fmt.Errorf("rebuild credit-dependent API documents: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE api_document_provenance SET projection_version=$2 WHERE entity_id=$1`, entityID, version); err != nil {
			return fmt.Errorf("version credit-dependent provenance: %w", err)
		}
		table := ""
		switch kind {
		case "movie":
			table = "canonical_movies"
		case "tv_show":
			table = "canonical_tv_shows"
		case "anime":
			table = "canonical_anime"
		}
		if table != "" {
			if _, err := tx.Exec(ctx, `UPDATE `+table+` SET document=jsonb_set(jsonb_set(document,'{data,credits}',$2::jsonb,true),'{projection_version}',to_jsonb($3::bigint),true),updated_at=now() WHERE entity_id=$1`, entityID, creditBody, version); err != nil {
				return fmt.Errorf("rebuild %s credit document: %w", kind, err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE search_entities SET projection_version=$2,summary=jsonb_set(summary,'{projection_version}',to_jsonb($2::bigint),true),updated_at=now() WHERE entity_id=$1`, entityID, version); err != nil {
			return fmt.Errorf("version credit-dependent search document: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version)VALUES($1,$2,$3,'updated',ARRAY['detail','credits','provenance'],$4)`, entityID, kind, slug, version); err != nil {
			return fmt.Errorf("publish credit-dependent change: %w", err)
		}
	}
	return nil
}
