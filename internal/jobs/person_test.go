package jobs

import (
	"context"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type redirectingPersonEnrichmentService struct {
	redirects map[string]string
	calls     []string
}

func (s *redirectingPersonEnrichmentService) CanonicalID(_ context.Context, id string) (string, error) {
	for s.redirects[id] != "" {
		id = s.redirects[id]
	}
	return id, nil
}

func (s *redirectingPersonEnrichmentService) EnrichTMDB(_ context.Context, entityID, _ string, _ int64, _ providercredentials.Credentials) error {
	s.calls = append(s.calls, "tmdb:"+entityID)
	s.redirects[entityID] = "survivor"
	return nil
}

func (s *redirectingPersonEnrichmentService) EnrichTVMaze(_ context.Context, entityID, _ string, _ int64) error {
	s.calls = append(s.calls, "tvmaze:"+entityID)
	return nil
}

func (s *redirectingPersonEnrichmentService) EnrichTVDB(_ context.Context, entityID, _ string, _ int64, _ providercredentials.Credentials) error {
	s.calls = append(s.calls, "tvdb:"+entityID)
	return nil
}

func TestPersonEnrichFollowsMergeBetweenProviders(t *testing.T) {
	t.Parallel()
	service := &redirectingPersonEnrichmentService{redirects: map[string]string{}}
	worker := &PersonEnrichWorker{service: service, runtime: &platform.Runtime{}}
	job := &river.Job[PersonEnrichArgs]{JobRow: &rivertype.JobRow{ID: 123}, Args: PersonEnrichArgs{
		EntityID: "retired",
		TMDBID:   "1",
		TVMazeID: "2",
		TVDBID:   "3",
	}}

	if err := worker.Work(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmdb:retired", "tvmaze:survivor", "tvdb:survivor"}
	if len(service.calls) != len(want) {
		t.Fatalf("provider calls: got %v want %v", service.calls, want)
	}
	for index := range want {
		if service.calls[index] != want[index] {
			t.Fatalf("provider calls: got %v want %v", service.calls, want)
		}
	}
}
