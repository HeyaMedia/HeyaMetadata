package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/releases"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const ReleaseIngestKind = "release_ingest_v1"

type ReleaseIngestArgs struct {
	MusicBrainzID string `json:"musicbrainz_id" river:"unique"`
	Reason        string `json:"reason,omitempty"`
}

func (ReleaseIngestArgs) Kind() string { return ReleaseIngestKind }
func (ReleaseIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertRelease(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args ReleaseIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2)WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority)
	return inserted, err
}

type ReleaseIngestWorker struct {
	river.WorkerDefaults[ReleaseIngestArgs]
	service *releases.Service
}

func NewReleaseIngestWorker(runtime *platform.Runtime) *ReleaseIngestWorker {
	return &ReleaseIngestWorker{service: releases.NewService(runtime)}
}
func (w *ReleaseIngestWorker) Work(ctx context.Context, job *river.Job[ReleaseIngestArgs]) error {
	_, err := w.service.IngestMusicBrainz(ctx, job.Args.MusicBrainzID, job.ID)
	if err == nil {
		return nil
	}
	var status *providers.StatusError
	if errors.As(err, &status) {
		if status.StatusCode == http.StatusNotFound {
			return river.JobCancel(err)
		}
		if status.StatusCode == http.StatusTooManyRequests {
			return river.JobSnooze(2 * time.Minute)
		}
	}
	return fmt.Errorf("ingest release: %w", err)
}
