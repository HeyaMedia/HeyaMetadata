package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/releases"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const ReleaseIngestKind = "release_ingest_v1"

type ReleaseIngestArgs struct {
	MusicBrainzID string `json:"musicbrainz_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (ReleaseIngestArgs) Kind() string { return ReleaseIngestKind }
func (ReleaseIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: MusicQueue, MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertRelease(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args ReleaseIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET queue=$4,priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true)END WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef, MusicQueue)
	if err == nil && tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, err
}

type ReleaseIngestWorker struct {
	river.WorkerDefaults[ReleaseIngestArgs]
	service *releases.Service
	runtime *platform.Runtime
}

func NewReleaseIngestWorker(runtime *platform.Runtime) *ReleaseIngestWorker {
	return &ReleaseIngestWorker{service: releases.NewService(runtime), runtime: runtime}
}
func (w *ReleaseIngestWorker) Work(ctx context.Context, job *river.Job[ReleaseIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestMusicBrainzWithCredentials(ctx, job.Args.MusicBrainzID, job.ID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
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
