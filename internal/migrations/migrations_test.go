package migrations

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreOrdered(t *testing.T) {
	t.Parallel()

	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) == 0 {
		t.Fatal("no embedded migrations")
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].Version >= migrations[i].Version {
			t.Fatalf("migration versions are not strictly increasing")
		}
	}
}

func TestDomainQueueMigrationRemapsWaitingBacklog(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var sql string
	for _, migration := range migrations {
		if migration.Version == 56 {
			sql = migration.SQL
			break
		}
	}
	if sql == "" {
		t.Fatal("domain queue migration is missing")
	}
	for _, value := range []string{"'music'", "'movie'", "'tv'", "'anime'", "'books'", "discovery_search_v1", "media_kind"} {
		if !strings.Contains(sql, value) {
			t.Errorf("domain queue migration does not contain %s", value)
		}
	}
}

func TestLibraryReadIndexMigrationExists(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version == 57 && strings.Contains(migration.SQL, "search_entities_kind_updated_idx") && strings.Contains(migration.SQL, "image_candidates_materialization_state_idx") {
			return
		}
	}
	t.Fatal("library read index migration is missing")
}

func TestSearchAndPersonReconciliationIndexMigrationExists(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version == 58 && strings.Contains(migration.SQL, "external_id_claims_accepted_value_idx") && strings.Contains(migration.SQL, "canonical_people_normalized_display_name_idx") {
			return
		}
	}
	t.Fatal("search and person reconciliation index migration is missing")
}

func TestPersonReconciliationRootIndexMigrationExists(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version == 59 && strings.Contains(migration.SQL, "external_id_claims_person_reconciliation_roots_idx") {
			return
		}
	}
	t.Fatal("person reconciliation root index migration is missing")
}

func TestCreditPersonProjectionMigrationSerializesCanonicalization(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version != 60 {
			continue
		}
		for _, value := range []string{
			"CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()",
			"pg_advisory_xact_lock",
			"heya:credit-projection-canonical-people",
			"heya_ensure_canonical_person",
		} {
			if !strings.Contains(migration.SQL, value) {
				t.Errorf("credit person projection migration does not contain %q", value)
			}
		}
		return
	}
	t.Fatal("credit person projection serialization migration is missing")
}

func TestCreditPersonLockMigrationUsesDeterministicCanonicalRoots(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version != 61 {
			continue
		}
		for _, value := range []string{
			"CREATE OR REPLACE FUNCTION heya_lock_credit_people(",
			"'canonical:' || claim.entity_id::text",
			"'provider:' || requested.provider",
			"SELECT lock_key FROM lock_roots ORDER BY lock_key",
			"pg_advisory_xact_lock",
			"CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()",
		} {
			if !strings.Contains(migration.SQL, value) {
				t.Errorf("deterministic credit person lock migration does not contain %q", value)
			}
		}
		return
	}
	t.Fatal("deterministic credit person lock migration is missing")
}

func TestProviderObservationIdentityIndexBoundsRequestKeys(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version != 62 {
			continue
		}
		for _, value := range []string{
			"DROP CONSTRAINT IF EXISTS provider_observations_provider_provider_namespace_provider__key",
			"CREATE UNIQUE INDEX provider_observations_identity_time_uidx",
			"md5(provider_record_id)",
			"md5(request_key)",
		} {
			if !strings.Contains(migration.SQL, value) {
				t.Errorf("provider observation identity migration does not contain %q", value)
			}
		}
		return
	}
	t.Fatal("bounded provider observation identity migration is missing")
}

func TestKnownCreditPeopleBecomeReadOnlyDuringProjection(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version != 63 {
			continue
		}
		for _, value := range []string{
			"person.entity_id IS NULL",
			"IF canonical_id IS NULL THEN",
			"heya:new-credit-person:",
			"IF needs_projection THEN",
			"ON CONFLICT(entity_id) DO NOTHING",
			"CREATE OR REPLACE FUNCTION heya_attach_canonical_person_to_credit()",
		} {
			if !strings.Contains(migration.SQL, value) {
				t.Errorf("read-only known credit person migration does not contain %q", value)
			}
		}
		return
	}
	t.Fatal("read-only known credit person migration is missing")
}

func TestColdReadyImageIndexMatchesMaintenanceQuery(t *testing.T) {
	t.Parallel()
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version == 64 &&
			strings.Contains(migration.SQL, "image_candidates_ready_cold_access_idx") &&
			strings.Contains(migration.SQL, "COALESCE(last_accessed_at, materialized_at, created_at)") &&
			strings.Contains(migration.SQL, "WHERE materialization_state = 'ready'") {
			return
		}
	}
	t.Fatal("cold ready image maintenance index is missing")
}
