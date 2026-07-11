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
	"github.com/HeyaMedia/HeyaMetadata/internal/sourcecollection"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const SourceCollectKind = "source_collect_v1"

type SourceCollectArgs struct {
	Provider           string `json:"provider" river:"unique"`
	IdentifierProvider string `json:"identifier_provider" river:"unique"`
	Namespace          string `json:"namespace" river:"unique"`
	Value              string `json:"value" river:"unique"`
	CredentialRef      string `json:"credential_ref,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

func (SourceCollectArgs) Kind() string { return SourceCollectKind }

func (SourceCollectArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5, Priority: PriorityInteractive,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()},
	}
}

func InsertSourceCollect(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args SourceCollectArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	commandTag, err := runtime.DB.Exec(ctx, `
		UPDATE river_job
		SET priority=LEAST(priority, $2),
			args=CASE WHEN $3='' THEN args ELSE jsonb_set(args, '{credential_ref}', to_jsonb($3::text), true) END
		WHERE id=$1 AND state IN ('available', 'pending', 'retryable', 'scheduled')`,
		inserted.Job.ID, priority, args.CredentialRef)
	if err != nil {
		return nil, fmt.Errorf("promote source collection job: %w", err)
	}
	if commandTag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, nil
}

type SourceCollectWorker struct {
	river.WorkerDefaults[SourceCollectArgs]
	runtime *platform.Runtime
}

func NewSourceCollectWorker(runtime *platform.Runtime) *SourceCollectWorker {
	return &SourceCollectWorker{runtime: runtime}
}

func (w *SourceCollectWorker) Work(ctx context.Context, job *river.Job[SourceCollectArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = sourcecollection.Collect(ctx, w.runtime, sourcecollection.Request{
		Provider: job.Args.Provider,
		Identifier: providers.Identifier{
			Provider: job.Args.IdentifierProvider, Namespace: job.Args.Namespace, Value: job.Args.Value,
		},
		JobID: job.ID, Credentials: credentials,
	})
	if err != nil {
		var statusError *providers.StatusError
		if errors.As(err, &statusError) {
			switch statusError.StatusCode {
			case http.StatusNotFound:
				_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
				return river.JobCancel(err)
			case http.StatusTooManyRequests:
				return river.JobSnooze(2 * time.Minute)
			}
		}
		return err
	}
	_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	return nil
}
