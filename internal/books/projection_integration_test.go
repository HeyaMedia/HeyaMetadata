package books

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationPublicationProjectionVersionsMatchOwningEntity(t *testing.T) {
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
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	suffix := fmt.Sprint(time.Now().UnixNano())
	var workID string
	if err := tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('book_work',$1)RETURNING id`, "projection-work-"+suffix).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	fresh := Freshness{State: "fresh", UpdatedAt: time.Now().UTC(), FreshUntil: time.Now().UTC().Add(24 * time.Hour)}
	document := Document{SchemaVersion: 2, ID: workID, Kind: KindBook, Slug: "projection-work-" + suffix, Freshness: fresh}
	document.Display.Title = "Projection Test Work"
	for expected := int64(1); expected <= 2; expected++ {
		document.ProjectionVersion, err = nextProjectionVersion(ctx, tx, workID)
		if err != nil {
			t.Fatal(err)
		}
		if document.ProjectionVersion != expected {
			t.Fatalf("work increment %d: got %d", expected, document.ProjectionVersion)
		}
		if err := upsertProjection(ctx, tx, document, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	assertProjectionVersions(t, ctx, tx, workID, 2)

	var authorID string
	if err := tx.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('author',$1)RETURNING id`, "projection-author-"+suffix).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	authorVersion, err := nextProjectionVersion(ctx, tx, authorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertAuthorProjection(ctx, tx, authorID, "projection-author-"+suffix, Author{Name: "Projection Test Author"}, fresh, authorVersion); err != nil {
		t.Fatal(err)
	}
	assertProjectionVersions(t, ctx, tx, authorID, 1)
}

func assertProjectionVersions(t *testing.T, ctx context.Context, tx pgx.Tx, entityID string, expected int64) {
	t.Helper()
	var entityVersion, detailVersion, searchVersion, embeddedVersion int64
	if err := tx.QueryRow(ctx, `
		SELECT e.canonical_version,d.projection_version,s.projection_version,
		       (d.document->>'projection_version')::bigint
		FROM entities e
		JOIN api_documents d ON d.entity_id=e.id AND d.document_kind='detail'
		JOIN search_entities s ON s.entity_id=e.id
		WHERE e.id=$1`, entityID).Scan(&entityVersion, &detailVersion, &searchVersion, &embeddedVersion); err != nil {
		t.Fatal(err)
	}
	if entityVersion != expected || detailVersion != expected || searchVersion != expected || embeddedVersion != expected {
		t.Fatalf("versions for %s: entity=%d detail=%d search=%d embedded=%d want=%d", entityID, entityVersion, detailVersion, searchVersion, embeddedVersion, expected)
	}
}
