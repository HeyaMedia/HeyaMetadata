package accessstats

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func TestIntegrationRefreshCadenceSkipsRowsLockedByIngestion(t *testing.T) {
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
	var entityID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version)VALUES('movie',$1,1)RETURNING id::text`, "integration-cadence-lock-"+suffix).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM provider_refresh_states WHERE entity_id=$1`, entityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=$1`, entityID)
	})

	lastSuccess := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_success_at,next_eligible_at)VALUES($1,'integration',$2,$2)`, entityID, lastSuccess); err != nil {
		t.Fatal(err)
	}
	blocker, err := runtime.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	if _, err := blocker.Exec(ctx, `UPDATE provider_refresh_states SET failure_message=failure_message WHERE entity_id=$1 AND provider='integration'`, entityID); err != nil {
		t.Fatal(err)
	}

	recalculateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := RecalculateRefreshCadence(recalculateCtx, runtime); err != nil {
		t.Fatalf("cadence sweep waited on an ingestion-locked row: %v", err)
	}
	var whileLocked time.Time
	if err := runtime.DB.QueryRow(ctx, `SELECT next_eligible_at FROM provider_refresh_states WHERE entity_id=$1 AND provider='integration'`, entityID).Scan(&whileLocked); err != nil {
		t.Fatal(err)
	}
	if !whileLocked.Equal(lastSuccess) {
		t.Fatalf("locked cadence row changed from %s to %s", lastSuccess, whileLocked)
	}

	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := RecalculateRefreshCadence(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	var afterUnlock time.Time
	if err := runtime.DB.QueryRow(ctx, `SELECT next_eligible_at FROM provider_refresh_states WHERE entity_id=$1 AND provider='integration'`, entityID).Scan(&afterUnlock); err != nil {
		t.Fatal(err)
	}
	want := lastSuccess.Add(30 * 24 * time.Hour)
	if !afterUnlock.Equal(want) {
		t.Fatalf("cadence after unlock = %s, want %s", afterUnlock, want)
	}
}
