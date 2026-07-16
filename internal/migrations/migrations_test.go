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
