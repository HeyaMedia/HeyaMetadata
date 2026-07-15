package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/migrations"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestChangeFeedHTTPResetContract(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres and Redis stack")
	}
	if err := config.LoadEnvFiles(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := migrations.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	handler := NewWithRuntime("integration", &platform.Runtime{DB: database, Redis: redisClient, Config: cfg}).Handler()

	current := httptest.NewRecorder()
	handler.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/api/v2/changes?after=0&limit=1", nil))
	if current.Code != http.StatusOK {
		t.Fatalf("current stream status=%d body=%s", current.Code, current.Body.String())
	}
	var page struct {
		StreamID   string `json:"stream_id"`
		HeadCursor int64  `json:"head_cursor"`
		Entries    []any  `json:"entries"`
		NextCursor int64  `json:"next_cursor"`
	}
	if err := json.NewDecoder(current.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(page.StreamID); err != nil || page.HeadCursor < 0 || page.NextCursor < 0 || page.Entries == nil {
		t.Fatalf("invalid current stream page: %+v", page)
	}

	aheadURL := fmt.Sprintf("/api/v2/changes?after=%d&stream_id=%s", page.HeadCursor+1, url.QueryEscape(page.StreamID))
	ahead := httptest.NewRecorder()
	handler.ServeHTTP(ahead, httptest.NewRequest(http.MethodGet, aheadURL, nil))
	assertChangeFeedProblem(t, ahead, "change_cursor_ahead", page.StreamID, page.HeadCursor)

	changedURL := fmt.Sprintf("/api/v2/changes?after=0&stream_id=%s", uuid.NewString())
	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, httptest.NewRequest(http.MethodGet, changedURL, nil))
	assertChangeFeedProblem(t, changed, "change_stream_changed", page.StreamID, page.HeadCursor)
}

func assertChangeFeedProblem(t *testing.T, response *httptest.ResponseRecorder, code, streamID string, head int64) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := response.Header().Get("Server-Timing"); got == "" {
		t.Fatal("missing Server-Timing conflict header")
	}
	var problem ErrorModel
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != code || problem.StreamID != streamID || problem.HeadCursor == nil || *problem.HeadCursor != head || problem.Status != http.StatusConflict {
		t.Fatalf("problem=%+v", problem)
	}
}
