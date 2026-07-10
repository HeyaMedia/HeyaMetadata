package migrations

import "testing"

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
