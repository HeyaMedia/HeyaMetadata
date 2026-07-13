package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/musicalworks"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const MusicalWorkIngestKind = "musical_work_ingest_v1"

type MusicalWorkIngestArgs struct {
	OpenOpusWorkID string `json:"openopus_work_id" river:"unique"`
	Reason         string `json:"reason,omitempty"`
}

func (MusicalWorkIngestArgs) Kind() string { return MusicalWorkIngestKind }

func (MusicalWorkIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

func InsertMusicalWork(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args MusicalWorkIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{reason}',to_jsonb($3::text),true)END WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.Reason)
	return inserted, err
}

type MusicalWorkIngestWorker struct {
	river.WorkerDefaults[MusicalWorkIngestArgs]
	service *musicalworks.Service
}

func NewMusicalWorkIngestWorker(runtime *platform.Runtime) *MusicalWorkIngestWorker {
	return &MusicalWorkIngestWorker{service: musicalworks.NewService(runtime)}
}

func (w *MusicalWorkIngestWorker) Work(ctx context.Context, job *river.Job[MusicalWorkIngestArgs]) error {
	_, err := w.service.IngestOpenOpus(ctx, job.Args.OpenOpusWorkID, job.ID)
	if err == nil {
		return nil
	}
	if errors.Is(err, musicalworks.ErrProviderNotFound) {
		return river.JobCancel(err)
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
	return fmt.Errorf("ingest Open Opus musical work: %w", err)
}
