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
	var showID string
	showSlug := "integration-credit-show-" + suffix
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version)VALUES('tv_show',$1,1)RETURNING id::text`, showSlug).Scan(&showID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupModerationCreditEntity(runtime, showID) })
	setupStatements := []string{
		`INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1::uuid,'tv_show',$2::text)`,
		`INSERT INTO search_entities(entity_id,kind,slug,display_title,summary,projection_version)VALUES($1::uuid,'tv_show',$2::text,'Integration Credit Show',jsonb_build_object('schema_version',1,'projection_version',1,'id',$1::text,'kind','tv_show','slug',$2::text,'display',jsonb_build_object('title','Integration Credit Show')),1)`,
		`INSERT INTO canonical_tv_shows(entity_id,merge_version,source_fingerprint,document)VALUES($1::uuid,'integration','integration',jsonb_build_object('schema_version',1,'projection_version',1,'id',$1::text,'kind','tv_show','slug',$2::text,'display',jsonb_build_object('title','Integration Credit Show'),'data',jsonb_build_object('credits','[]'::jsonb)))`,
		`INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1::uuid,'detail',1,1,jsonb_build_object('schema_version',1,'projection_version',1,'id',$1::text,'kind','tv_show','slug',$2::text,'display',jsonb_build_object('title','Integration Credit Show'),'data',jsonb_build_object('credits','[]'::jsonb)),now()+interval '1 day'),($1::uuid,'summary',1,1,jsonb_build_object('schema_version',1,'projection_version',1,'id',$1::text,'kind','tv_show','slug',$2::text,'display',jsonb_build_object('title','Integration Credit Show')),now()+interval '1 day')`,
	}
	for _, statement := range setupStatements {
		if _, err := runtime.DB.Exec(ctx, statement, showID, showSlug); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO entity_credit_projections(entity_id,provider,provider_person_id,display_name,credit_type,character_name,credit_order,projection_version)VALUES($1,'integration_a',$2,'Integration Person','cast','Self - Host',0,1),($1,'integration_b',$2,'Integration Person','cast','Integration Person',0,1)`, showID, suffix); err != nil {
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
	var claims, credits, redirects, audits, projectedCredits, documentCredits, dependentChanges int
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
	var entityVersion, apiVersion, searchVersion int64
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*),min(person_entity_id::text),max(projection_version) FROM entity_credit_projections WHERE entity_id=$1`, showID).Scan(&projectedCredits, &resolved, &apiVersion); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT jsonb_array_length(document#>'{data,credits}'),projection_version FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, showID).Scan(&documentCredits, &apiVersion); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT canonical_version FROM entities WHERE id=$1`, showID).Scan(&entityVersion); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT projection_version FROM search_entities WHERE entity_id=$1`, showID).Scan(&searchVersion); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM change_outbox WHERE entity_id=$1 AND changed_scopes@>ARRAY['credits']::text[]`, showID).Scan(&dependentChanges); err != nil {
		t.Fatal(err)
	}
	if claims != 2 || credits != 2 || redirects != 1 || audits != 1 || projectedCredits != 1 || documentCredits != 1 || resolved != survivorID || entityVersion <= 1 || apiVersion != entityVersion || searchVersion != entityVersion || dependentChanges != 1 {
		t.Fatalf("claims=%d credits=%d redirects=%d audits=%d projected=%d document=%d resolved=%s versions=%d/%d/%d changes=%d", claims, credits, redirects, audits, projectedCredits, documentCredits, resolved, entityVersion, apiVersion, searchVersion, dependentChanges)
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

func TestIntegrationReconcileAutomaticallyAcceptsIndependentExternalIDEvidence(t *testing.T) {
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
	name := "Integration Verified Person " + suffix
	var tmdbID, tvdbID string
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('tmdb',$1,$2,NULL)::text`, suffix, name).Scan(&tmdbID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT heya_ensure_canonical_person('tvdb',$1,$2,NULL)::text`, suffix, name).Scan(&tvdbID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupModerationPeople(runtime, tmdbID, tvdbID) })
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO person_identity_evidence(person_entity_id,provider,namespace,normalized_value,source_provider)VALUES($1,'imdb','name',$3,'tmdb'),($2,'imdb','name',$3,'tvdb')`, tmdbID, tvdbID, "nm"+suffix); err != nil {
		t.Fatal(err)
	}

	service := NewService(runtime)
	if _, err := service.ReconciliationRoots(ctx, 1); err != nil {
		t.Fatalf("select scheduled reconciliation roots: %v", err)
	}
	if err := service.Reconcile(ctx, tvdbID); err != nil {
		t.Fatal(err)
	}
	resolved, err := service.CanonicalID(ctx, tvdbID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != tmdbID {
		t.Fatalf("resolved ID = %s, want TMDB-root survivor %s", resolved, tmdbID)
	}
	var accepted, audits int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM person_reconciliation_candidates WHERE left_person_id=LEAST($1::uuid,$2::uuid) AND right_person_id=GREATEST($1::uuid,$2::uuid) AND state='accepted' AND decided_by='system:person-reconciler'`, tmdbID, tvdbID).Scan(&accepted); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM moderation_audit_log WHERE entity_kind='person' AND action='person_reconciliation_accept' AND subject_ids@>ARRAY[$1::uuid,$2::uuid]`, tmdbID, tvdbID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || audits != 1 {
		t.Fatalf("accepted=%d audits=%d", accepted, audits)
	}
}

func TestIntegrationCreditPersonCanonicalizationIsSerialized(t *testing.T) {
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
	provider := "integration_credit_lock"
	var firstEntityID, secondEntityID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version)VALUES('movie',$1,1)RETURNING id::text`, "integration-credit-lock-a-"+suffix).Scan(&firstEntityID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version)VALUES('movie',$1,1)RETURNING id::text`, "integration-credit-lock-b-"+suffix).Scan(&secondEntityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=ANY($1::uuid[])`, []string{firstEntityID, secondEntityID})
		var personIDs []string
		rows, queryErr := runtime.DB.Query(context.Background(), `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='person' AND provider=$1 AND namespace='person' AND normalized_value IN($2,$3)`, provider, "a-"+suffix, "b-"+suffix)
		if queryErr == nil {
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					personIDs = append(personIDs, id)
				}
			}
			rows.Close()
		}
		cleanupModerationPeople(runtime, personIDs...)
	})

	firstTx, err := runtime.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstTx.Rollback(context.Background()) })
	if _, err := firstTx.Exec(ctx, `INSERT INTO entity_credit_projections(entity_id,provider,provider_person_id,display_name,credit_type,credit_order,projection_version)VALUES($1,$2,$3,'Person A','cast',0,1)`, firstEntityID, provider, "a-"+suffix); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		secondCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		secondTx, beginErr := runtime.DB.Begin(secondCtx)
		if beginErr != nil {
			finished <- beginErr
			return
		}
		defer secondTx.Rollback(context.Background())
		close(started)
		if _, execErr := secondTx.Exec(secondCtx, `INSERT INTO entity_credit_projections(entity_id,provider,provider_person_id,display_name,credit_type,credit_order,projection_version)VALUES($1,$2,$3,'Person B','cast',0,1),($1,$2,$4,'Person A','cast',1,1)`, secondEntityID, provider, "b-"+suffix, "a-"+suffix); execErr != nil {
			finished <- execErr
			return
		}
		finished <- secondTx.Commit(secondCtx)
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatalf("second credit projection completed before the first transaction released its canonical-person lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if _, err := firstTx.Exec(ctx, `INSERT INTO entity_credit_projections(entity_id,provider,provider_person_id,display_name,credit_type,credit_order,projection_version)VALUES($1,$2,$3,'Person B','cast',1,1)`, firstEntityID, provider, "b-"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatalf("second reverse-order credit projection failed after the first committed: %v", err)
	}

	var projected int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_credit_projections WHERE entity_id=ANY($1::uuid[])`, []string{firstEntityID, secondEntityID}).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if projected != 4 {
		t.Fatalf("projected credits = %d, want 4", projected)
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

func cleanupModerationCreditEntity(runtime *platform.Runtime, id string) {
	ctx := context.Background()
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_log WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_outbox WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM api_document_provenance WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM api_documents WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_credit_projections WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM canonical_tv_shows WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_entities WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_slugs WHERE entity_id=$1`, id)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM entities WHERE id=$1`, id)
}
