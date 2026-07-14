package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateFreshDatabase(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres stack")
	}
	if err := config.LoadEnvFiles(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	adminConfig := baseConfig.Copy()
	adminConfig.ConnConfig.Database = "postgres"
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	databaseName := fmt.Sprintf("heya_metadata_migration_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop fresh migration database: %v", err)
		}
	}()

	freshConfig := baseConfig.Copy()
	freshConfig.ConnConfig.Database = databaseName
	database, err := pgxpool.NewWithConfig(ctx, freshConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	result, err := Migrate(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Application) != len(loaded) {
		t.Fatalf("application migrations: got %d, want %d", len(result.Application), len(loaded))
	}
	if len(result.River.Versions) == 0 {
		t.Fatal("fresh database applied no River migrations")
	}

	status, err := AppStatus(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != status.Latest || len(status.Pending) != 0 {
		t.Fatalf("application migration status: %+v", status)
	}
	var riverInstalled bool
	if err := database.QueryRow(ctx, `SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&riverInstalled); err != nil {
		t.Fatal(err)
	}
	if !riverInstalled {
		t.Fatal("River schema was not installed")
	}

	second, err := Migrate(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Application) != 0 || len(second.River.Versions) != 0 {
		t.Fatalf("second migration was not idempotent: application=%d river=%d", len(second.Application), len(second.River.Versions))
	}
}
