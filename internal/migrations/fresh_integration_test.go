package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
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
	var firstStreamID string
	if err := database.QueryRow(ctx, `SELECT stream_id::text FROM change_cursor WHERE singleton=true`).Scan(&firstStreamID); err != nil {
		t.Fatal(err)
	}
	runtime := &platform.Runtime{DB: database}
	empty, err := changelog.ReadPage(ctx, runtime, firstStreamID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if empty.StreamID != firstStreamID || empty.Head != 0 || empty.Next != 0 || len(empty.Entries) != 0 {
		t.Fatalf("unexpected empty change stream: %+v", empty)
	}
	for index := 1; index <= 3; index++ {
		var entityID string
		if err := database.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version) VALUES('movie',$1,$2) RETURNING id::text`, fmt.Sprintf("change-stream-fixture-%d", index), index).Scan(&entityID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,scope,change_type,changed_scopes,projection_version,committed_at) VALUES($1,'movie',$2,'public','updated',ARRAY['detail']::text[],$3,now()+($4 * interval '1 millisecond'))`, entityID, fmt.Sprintf("change-stream-fixture-%d", index), index, index); err != nil {
			t.Fatal(err)
		}
	}
	if err := changelog.Sequence(ctx, runtime, 100); err != nil {
		t.Fatal(err)
	}
	firstPage, err := changelog.ReadPage(ctx, runtime, firstStreamID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.Head != 3 || firstPage.Next != 2 || len(firstPage.Entries) != 2 {
		t.Fatalf("unexpected first change page: %+v", firstPage)
	}
	replayed, err := changelog.ReadPage(ctx, runtime, firstStreamID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstPage.Entries, replayed.Entries) || firstPage.Next != replayed.Next || firstPage.Head != replayed.Head {
		t.Fatalf("change replay was not deterministic: first=%+v replay=%+v", firstPage, replayed)
	}
	lastPage, err := changelog.ReadPage(ctx, runtime, firstStreamID, firstPage.Next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if lastPage.Next != lastPage.Head || len(lastPage.Entries) != 1 {
		t.Fatalf("pagination skipped or duplicated a change: %+v", lastPage)
	}
	atHead, err := changelog.ReadPage(ctx, runtime, firstStreamID, lastPage.Head, 100)
	if err != nil {
		t.Fatal(err)
	}
	if atHead.Next != lastPage.Head || len(atHead.Entries) != 0 {
		t.Fatalf("cursor at head should return an empty page: %+v", atHead)
	}
	var conflict *changelog.CursorConflict
	if err := func() error {
		_, readErr := changelog.ReadPage(ctx, runtime, firstStreamID, lastPage.Head+1, 100)
		return readErr
	}(); !errors.As(err, &conflict) || conflict.Code != "change_cursor_ahead" {
		t.Fatalf("ahead cursor error = %v", err)
	}
	// Model restoring an older snapshot: the stream identity survives, but its
	// public head moves behind a cursor already persisted by the consumer.
	if _, err := database.Exec(ctx, `DELETE FROM change_log WHERE sequence=$1`, lastPage.Head); err != nil {
		t.Fatal(err)
	}
	conflict = nil
	if _, err := changelog.ReadPage(ctx, runtime, firstStreamID, lastPage.Head, 100); !errors.As(err, &conflict) || conflict.Code != "change_cursor_ahead" || conflict.Head != lastPage.Head-1 {
		t.Fatalf("snapshot rollback error = %v conflict=%+v", err, conflict)
	}

	second, err := Migrate(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Application) != 0 || len(second.River.Versions) != 0 {
		t.Fatalf("second migration was not idempotent: application=%d river=%d", len(second.Application), len(second.River.Versions))
	}
	var preservedStreamID string
	if err := database.QueryRow(ctx, `SELECT stream_id::text FROM change_cursor WHERE singleton=true`).Scan(&preservedStreamID); err != nil {
		t.Fatal(err)
	}
	if preservedStreamID != firstStreamID {
		t.Fatalf("normal migration changed stream identity: %s -> %s", firstStreamID, preservedStreamID)
	}
	reopened, err := pgxpool.NewWithConfig(ctx, freshConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	var reopenedStreamID string
	if err := reopened.QueryRow(ctx, `SELECT stream_id::text FROM change_cursor WHERE singleton=true`).Scan(&reopenedStreamID); err != nil {
		reopened.Close()
		t.Fatal(err)
	}
	reopened.Close()
	if reopenedStreamID != firstStreamID {
		t.Fatalf("reopening the database changed stream identity: %s -> %s", firstStreamID, reopenedStreamID)
	}

	secondDatabaseName := fmt.Sprintf("heya_metadata_migration_test_second_%d", time.Now().UnixNano())
	secondIdentifier := pgx.Identifier{secondDatabaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+secondIdentifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+secondIdentifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop second fresh migration database: %v", err)
		}
	}()
	secondFreshConfig := baseConfig.Copy()
	secondFreshConfig.ConnConfig.Database = secondDatabaseName
	secondDatabase, err := pgxpool.NewWithConfig(ctx, secondFreshConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDatabase.Close()
	if _, err := Migrate(ctx, secondDatabase); err != nil {
		t.Fatal(err)
	}
	var newStreamID string
	if err := secondDatabase.QueryRow(ctx, `SELECT stream_id::text FROM change_cursor WHERE singleton=true`).Scan(&newStreamID); err != nil {
		t.Fatal(err)
	}
	if newStreamID == firstStreamID {
		t.Fatalf("independent databases reused change stream identity %s", firstStreamID)
	}
}
