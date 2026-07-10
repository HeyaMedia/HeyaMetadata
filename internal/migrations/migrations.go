package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

const migrationLockID int64 = 0x484559414d455441 // "HEYAMETA"

type Migration struct {
	Version int64
	Name    string
	SQL     string
}

type Status struct {
	Current int64
	Latest  int64
	Pending []Migration
}

func Load() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "sql")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(entry.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		contents, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, Migration{Version: version, Name: parts[1], SQL: string(contents)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].Version == migrations[i].Version {
			return nil, fmt.Errorf("duplicate migration version %d", migrations[i].Version)
		}
	}
	return migrations, nil
}

func AppStatus(ctx context.Context, pool *pgxpool.Pool) (Status, error) {
	migrations, err := Load()
	if err != nil {
		return Status{}, err
	}
	status := Status{}
	if len(migrations) > 0 {
		status.Latest = migrations[len(migrations)-1].Version
	}

	var tableExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.heya_schema_migrations') IS NOT NULL`).Scan(&tableExists); err != nil {
		return Status{}, fmt.Errorf("inspect migration table: %w", err)
	}
	applied := map[int64]bool{}
	if tableExists {
		rows, err := pool.Query(ctx, `SELECT version FROM heya_schema_migrations ORDER BY version`)
		if err != nil {
			return Status{}, fmt.Errorf("read applied migrations: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var version int64
			if err := rows.Scan(&version); err != nil {
				return Status{}, fmt.Errorf("scan applied migration: %w", err)
			}
			applied[version] = true
			if version > status.Current {
				status.Current = version
			}
		}
		if err := rows.Err(); err != nil {
			return Status{}, fmt.Errorf("read applied migrations: %w", err)
		}
	}
	for _, migration := range migrations {
		if !applied[migration.Version] {
			status.Pending = append(status.Pending, migration)
		}
	}
	return status, nil
}

func MigrateApp(ctx context.Context, pool *pgxpool.Pool) ([]Migration, error) {
	migrations, err := Load()
	if err != nil {
		return nil, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS heya_schema_migrations (
            version bigint PRIMARY KEY,
            name text NOT NULL,
            applied_at timestamptz NOT NULL DEFAULT now()
        )`); err != nil {
		return nil, fmt.Errorf("create migration table: %w", err)
	}

	applied := map[int64]bool{}
	rows, err := tx.Query(ctx, `SELECT version FROM heya_schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read migration versions: %w", err)
	}
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read migration versions: %w", err)
	}
	rows.Close()

	var executed []Migration
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		if _, err := tx.Exec(ctx, migration.SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
			return nil, fmt.Errorf("apply migration %04d_%s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO heya_schema_migrations (version, name) VALUES ($1, $2)`,
			migration.Version, migration.Name,
		); err != nil {
			return nil, fmt.Errorf("record migration %04d_%s: %w", migration.Version, migration.Name, err)
		}
		executed = append(executed, migration)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit migrations: %w", err)
	}
	return executed, nil
}

func MigrateRiver(ctx context.Context, pool *pgxpool.Pool) (*rivermigrate.MigrateResult, error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("create River migrator: %w", err)
	}
	result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("migrate River: %w", err)
	}
	return result, nil
}
