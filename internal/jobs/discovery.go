package jobs

import (
	"context"
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const DiscoverySearchKind = "discovery_search_v1"

type DiscoverySearchArgs struct {
	RequestHash   string `json:"request_hash" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

func (DiscoverySearchArgs) Kind() string { return DiscoverySearchKind }
func (DiscoverySearchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 4, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertDiscovery(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], run discovery.Run, credentialRef string) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, DiscoverySearchArgs{RequestHash: run.RequestHash, CredentialRef: credentialRef}, &river.InsertOpts{Priority: PriorityInteractive})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true) END WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, PriorityInteractive, credentialRef)
	if err != nil {
		return nil, fmt.Errorf("promote discovery job: %w", err)
	}
	if tag.RowsAffected() == 0 && credentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, credentialRef)
	}
	if err := discovery.AttachJob(ctx, runtime, run.RequestHash, inserted.Job.ID); err != nil {
		return nil, err
	}
	return inserted, nil
}

type DiscoverySearchWorker struct {
	river.WorkerDefaults[DiscoverySearchArgs]
	runtime *platform.Runtime
	service *discovery.Service
}

func NewDiscoverySearchWorker(runtime *platform.Runtime) *DiscoverySearchWorker {
	return &DiscoverySearchWorker{runtime: runtime, service: discovery.NewService(runtime)}
}
func (w *DiscoverySearchWorker) Work(ctx context.Context, job *river.Job[DiscoverySearchArgs]) (returnErr error) {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	run, err := discovery.GetRunByHash(ctx, w.runtime, job.Args.RequestHash)
	if err != nil {
		return river.JobCancel(err)
	}
	if run.State == "completed" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
		return nil
	}
	if err := discovery.Start(ctx, w.runtime, run.RequestHash); err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			discovery.Fail(ctx, w.runtime, run.RequestHash, returnErr)
		}
	}()
	var result discovery.Result
	switch run.Request.Kind {
	case discovery.KindArtist:
		result, err = w.service.DiscoverArtist(ctx, run.Request, job.ID)
	case discovery.KindMovie:
		result, err = w.service.DiscoverMovie(ctx, run.Request, job.ID, credentials.APIKey("tmdb"))
	case discovery.KindReleaseGroup:
		result, err = w.service.DiscoverReleaseGroup(ctx, run.Request, job.ID)
	case discovery.KindTVShow:
		result, err = w.service.DiscoverTV(ctx, run.Request, job.ID)
	case discovery.KindAnime:
		result, err = w.service.DiscoverAnime(ctx, run.Request, job.ID)
	default:
		return fmt.Errorf("discovery kind %q is not implemented", run.Request.Kind)
	}
	if err != nil {
		return err
	}
	if err = discovery.Complete(ctx, w.runtime, run.RequestHash, result); err != nil {
		return err
	}
	_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	return nil
}
