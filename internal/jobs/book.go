package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/books"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const BookIngestKind = "book_ingest_v1"

type BookIngestArgs struct {
	OpenLibraryWorkID string `json:"openlibrary_work_id" river:"unique"`
	CredentialRef     string `json:"credential_ref,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

func (BookIngestArgs) Kind() string { return BookIngestKind }
func (BookIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertBook(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args BookIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
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

type BookIngestWorker struct {
	river.WorkerDefaults[BookIngestArgs]
	runtime *platform.Runtime
	service *books.Service
}

func NewBookIngestWorker(runtime *platform.Runtime) *BookIngestWorker {
	return &BookIngestWorker{runtime: runtime, service: books.NewService(runtime)}
}
func (w *BookIngestWorker) Work(ctx context.Context, job *river.Job[BookIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestWork(ctx, job.Args.OpenLibraryWorkID, job.ID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
		return nil
	}
	return fmt.Errorf("ingest book work: %w", err)
}
