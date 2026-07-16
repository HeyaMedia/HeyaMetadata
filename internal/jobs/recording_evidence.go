package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const RecordingEvidenceRefreshKind = "recording_evidence_refresh_v1"

type RecordingEvidenceRefreshArgs struct {
	RecordingID string `json:"recording_id" river:"unique"`
}

func (RecordingEvidenceRefreshArgs) Kind() string { return RecordingEvidenceRefreshKind }
func (RecordingEvidenceRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: MusicQueue, Priority: PriorityScheduled, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

func InsertRecordingEvidenceRefresh(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], entityID string) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, RecordingEvidenceRefreshArgs{RecordingID: entityID}, nil)
	if err != nil {
		return nil, err
	}
	if _, err = runtime.DB.Exec(ctx, `UPDATE river_job SET queue=$2 WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, MusicQueue); err != nil {
		return nil, err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$2 WHERE entity_id=$1 AND provider='lrclib'`, entityID, inserted.Job.ID)
	return inserted, err
}

type RecordingEvidenceRefreshWorker struct {
	river.WorkerDefaults[RecordingEvidenceRefreshArgs]
	service *recordings.Service
}

func NewRecordingEvidenceRefreshWorker(runtime *platform.Runtime) *RecordingEvidenceRefreshWorker {
	return &RecordingEvidenceRefreshWorker{service: recordings.NewService(runtime)}
}

func (w *RecordingEvidenceRefreshWorker) Work(ctx context.Context, job *river.Job[RecordingEvidenceRefreshArgs]) error {
	if err := w.service.RefreshLyricsEvidence(ctx, job.Args.RecordingID, job.ID); err != nil {
		return fmt.Errorf("refresh recording evidence: %w", err)
	}
	return nil
}
