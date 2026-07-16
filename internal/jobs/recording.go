package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const RecordingIngestKind = "recording_ingest_v1"

type RecordingIngestArgs struct {
	MusicBrainzID string `json:"musicbrainz_id" river:"unique"`
	Reason        string `json:"reason,omitempty"`
}

func (RecordingIngestArgs) Kind() string { return RecordingIngestKind }
func (RecordingIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: MusicQueue, MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

func InsertRecording(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args RecordingIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE river_job SET queue=$4,priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{reason}',to_jsonb($3::text),true)END WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.Reason, MusicQueue)
	return inserted, err
}

type RecordingIngestWorker struct {
	river.WorkerDefaults[RecordingIngestArgs]
	service *recordings.Service
}

func NewRecordingIngestWorker(runtime *platform.Runtime) *RecordingIngestWorker {
	return &RecordingIngestWorker{service: recordings.NewService(runtime)}
}

func (w *RecordingIngestWorker) Work(ctx context.Context, job *river.Job[RecordingIngestArgs]) error {
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
	return fmt.Errorf("ingest recording: %w", err)
}
