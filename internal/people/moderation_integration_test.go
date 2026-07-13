package people

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func TestIntegrationAcceptReconciliationMovesIdentityAndLeavesRedirect(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	suffix := fmt.Sprint(time.Now().UnixNano())
	var survivorID, retiredID string
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('integration_a',$1,'Integration Person',NULL)::text`, suffix).Scan(&survivorID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('integration_b',$1,'Integration Person',NULL)::text`, suffix).Scan(&retiredID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupModerationPeople(runtime, survivorID, retiredID) })
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO person_provider_credits(person_entity_id,provider,provider_target_id,media_kind,title,credit_type,observed_at)VALUES($1,'integration_a','1','movie','Left Film','cast',now()),($2,'integration_b','2','tv_show','Right Show','crew',now())`, survivorID, retiredID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO person_reconciliation_candidates(left_person_id,right_person_id,score,reasons)VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),.95,'["integration"]')`, survivorID, retiredID); err != nil {
		t.Fatal(err)
	}
	service := NewService(runtime)
	decision, err := service.AcceptReconciliation(ctx, survivorID, retiredID, survivorID, "integration-test", "verified synthetic identity")
	if err != nil {
		t.Fatal(err)
	}
	if decision.SurvivorID != survivorID || decision.RetiredID != retiredID || decision.AuditLogID == "" {
		t.Fatalf("decision: %+v", decision)
	}
	second, err := service.AcceptReconciliation(ctx, survivorID, retiredID, survivorID, "integration-test", "verified synthetic identity")
	if err != nil || second.AuditLogID != decision.AuditLogID {
		t.Fatalf("idempotent acceptance: first=%+v second=%+v err=%v", decision, second, err)
	}
	resolved, err := service.CanonicalID(ctx, retiredID)
	if err != nil || resolved != survivorID {
		t.Fatalf("redirect resolved=%s err=%v", resolved, err)
	}
	var claims, credits, redirects, audits int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM external_id_claims WHERE entity_id=$1 AND provider IN('integration_a','integration_b')`, survivorID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM person_provider_credits WHERE person_entity_id=$1 AND provider IN('integration_a','integration_b')`, survivorID).Scan(&credits); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_redirects WHERE retired_entity_id=$1 AND survivor_entity_id=$2`, retiredID, survivorID).Scan(&redirects); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM moderation_audit_log WHERE id=$1 AND action='person_reconciliation_accept'`, decision.AuditLogID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if claims != 2 || credits != 2 || redirects != 1 || audits != 1 {
		t.Fatalf("claims=%d credits=%d redirects=%d audits=%d", claims, credits, redirects, audits)
	}
}

func TestIntegrationRejectReconciliationPreservesBothPeople(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	suffix := fmt.Sprint(time.Now().UnixNano())
	var leftID, rightID string
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('integration_reject_a',$1,'Same Name',NULL)::text`, suffix).Scan(&leftID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('integration_reject_b',$1,'Same Name',NULL)::text`, suffix).Scan(&rightID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupModerationPeople(runtime, leftID, rightID) })
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO person_reconciliation_candidates(left_person_id,right_person_id,score,reasons)VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),.8,'["integration"]')`, leftID, rightID); err != nil {
		t.Fatal(err)
	}
	service := NewService(runtime)
	decision, err := service.RejectReconciliation(ctx, leftID, rightID, "integration-test", "verified different people")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.RejectReconciliation(ctx, leftID, rightID, "integration-test", "verified different people")
	if err != nil || second.AuditLogID != decision.AuditLogID {
		t.Fatalf("idempotent rejection: first=%+v second=%+v err=%v", decision, second, err)
	}
	var active, redirects int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entities WHERE id=ANY($1::uuid[]) AND deleted_at IS NULL`, []string{leftID, rightID}).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_redirects WHERE retired_entity_id=ANY($1::uuid[])`, []string{leftID, rightID}).Scan(&redirects); err != nil {
		t.Fatal(err)
	}
	if decision.State != "rejected" || decision.AuditLogID == "" || active != 2 || redirects != 0 {
		t.Fatalf("decision=%+v active=%d redirects=%d", decision, active, redirects)
	}
}

func cleanupModerationPeople(runtime *platform.Runtime, ids ...string) {
	ctx := context.Background()
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_log WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_outbox WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_redirects WHERE retired_entity_id=ANY($1::uuid[]) OR survivor_entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM person_reconciliation_candidates WHERE left_person_id=ANY($1::uuid[]) OR right_person_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM moderation_audit_log WHERE subject_ids && $1::uuid[]`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_credit_projections WHERE person_entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM person_provider_credits WHERE person_entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_refresh_states WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM normalized_records WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM image_candidates WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_names WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_entities WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM external_id_claims WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_access_stats WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_slugs WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM canonical_people WHERE entity_id=ANY($1::uuid[])`, ids)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entities WHERE id=ANY($1::uuid[])`, ids)
}
