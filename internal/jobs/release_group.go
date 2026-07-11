package jobs

import (
	"context"
	"errors"
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"net/http"
	"time"
)

const ReleaseGroupIngestKind = "release_group_ingest_v1"

type ReleaseGroupIngestArgs struct {
	MusicBrainzID string `json:"musicbrainz_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (ReleaseGroupIngestArgs) Kind() string { return ReleaseGroupIngestKind }
func (ReleaseGroupIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertReleaseGroup(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args ReleaseGroupIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true) END WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, nil
}

type ReleaseGroupIngestWorker struct {
	river.WorkerDefaults[ReleaseGroupIngestArgs]
	service *releasegroups.Service
	runtime *platform.Runtime
}

func NewReleaseGroupIngestWorker(runtime *platform.Runtime) *ReleaseGroupIngestWorker {
	return &ReleaseGroupIngestWorker{service: releasegroups.NewService(runtime), runtime: runtime}
}
func (w *ReleaseGroupIngestWorker) Work(ctx context.Context, job *river.Job[ReleaseGroupIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestMusicBrainz(ctx, job.Args.MusicBrainzID, job.ID, credentials)
	if err != nil {
		var status *providers.StatusError
		if errors.As(err, &status) {
			if status.StatusCode == http.StatusNotFound {
				return river.JobCancel(err)
			}
			if status.StatusCode == http.StatusTooManyRequests {
				return river.JobSnooze(2 * time.Minute)
			}
		}
		return fmt.Errorf("ingest release group: %w", err)
	}
	_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	return nil
}
