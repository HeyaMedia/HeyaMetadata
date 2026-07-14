package anime

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func TestIntegrationSeriesEvidenceRebindsBroadIdentityToAnimeRoot(t *testing.T) {
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

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	rootAniDBID := suffix
	seasonAniDBID := suffix + "2"
	tvdbID := suffix + "3"
	imdbID := "tt" + suffix
	tmdbID := suffix + "4"
	var rootID, seasonID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('anime',$1)RETURNING id::text`, "anime-root-integration-"+suffix).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('anime',$1)RETURNING id::text`, "anime-season-integration-"+suffix).Scan(&seasonID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM episodic_series_external_evidence WHERE entity_kind='anime' AND anchor_value=$1`, tvdbID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM external_id_claims WHERE entity_id=ANY($1::uuid[])`, []string{rootID, seasonID})
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=ANY($1::uuid[])`, []string{rootID, seasonID})
	})

	now := time.Now().UTC()
	for _, claim := range []struct{ entityID, provider, namespace, value string }{
		{rootID, "anidb", "anime", rootAniDBID},
		{rootID, "tmdb", "tv", tmdbID},
		{rootID, "tvdb", "series", tvdbID},
		{seasonID, "anidb", "anime", seasonAniDBID},
		// Reproduce the historical bad binding before recording its series scope.
		{seasonID, "imdb", "title", imdbID},
	} {
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,first_observed_at,last_observed_at)VALUES($1,'anime',$2,$3,$4,'accepted',$5,$5)`, claim.entityID, claim.provider, claim.namespace, claim.value, now); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(runtime)
	if err := service.rememberSeriesExternalEvidence(ctx, tvdbID, rootAniDBID, []episodic.ExternalID{{Provider: "imdb", Namespace: "title", Value: imdbID}}, "", now); err != nil {
		t.Fatal(err)
	}
	var claimedEntityID string
	if err := runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='anime' AND provider='imdb' AND namespace='title' AND normalized_value=$1 AND state='accepted'`, imdbID).Scan(&claimedEntityID); err != nil {
		t.Fatal(err)
	}
	if claimedEntityID != rootID {
		t.Fatalf("IMDb claim remained on season entity: got %s, want %s", claimedEntityID, rootID)
	}

	var distinctEntities int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(DISTINCT entity_id) FROM external_id_claims WHERE entity_kind='anime' AND state='accepted' AND ((provider='anidb' AND namespace='anime' AND normalized_value=$1) OR (provider='imdb' AND namespace='title' AND normalized_value=$2) OR (provider='tmdb' AND namespace='tv' AND normalized_value=$3) OR (provider='tvdb' AND namespace='series' AND normalized_value=$4))`, rootAniDBID, imdbID, tmdbID, tvdbID).Scan(&distinctEntities); err != nil {
		t.Fatal(err)
	}
	if distinctEntities != 1 {
		t.Fatalf("root identifier evidence spans %d entities", distinctEntities)
	}
}
