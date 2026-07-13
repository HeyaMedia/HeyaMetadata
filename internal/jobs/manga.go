package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/manga"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const MangaIngestKind = "manga_ingest_v1"

type MangaIngestArgs struct {
	KitsuMangaID  string `json:"kitsu_manga_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (MangaIngestArgs) Kind() string { return MangaIngestKind }
func (MangaIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertManga(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args MangaIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true)END WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef)
	if err == nil && tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, err
}

type MangaIngestWorker struct {
	river.WorkerDefaults[MangaIngestArgs]
	runtime *platform.Runtime
	service *manga.Service
}

func NewMangaIngestWorker(runtime *platform.Runtime) *MangaIngestWorker {
	return &MangaIngestWorker{runtime: runtime, service: manga.NewService(runtime)}
}
func (w *MangaIngestWorker) Work(ctx context.Context, job *river.Job[MangaIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestKitsu(ctx, job.Args.KitsuMangaID, job.ID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
		return nil
	}
	return fmt.Errorf("ingest manga: %w", err)
}
