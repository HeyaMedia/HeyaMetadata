package jobs

import (
	"context"
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const DiscoverySearchKind = "discovery_search_v1"

type DiscoverySearchArgs struct {
	RequestHash string `json:"request_hash" river:"unique"`
}

func (DiscoverySearchArgs) Kind() string { return DiscoverySearchKind }
func (DiscoverySearchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 4, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertDiscovery(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], run discovery.Run) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, DiscoverySearchArgs{RequestHash: run.RequestHash}, &river.InsertOpts{Priority: PriorityInteractive})
	if err != nil {
		return nil, err
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
	run, err := discovery.GetRunByHash(ctx, w.runtime, job.Args.RequestHash)
	if err != nil {
		return river.JobCancel(err)
	}
	if run.State == "completed" {
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
	default:
		return fmt.Errorf("discovery kind %q is not implemented", run.Request.Kind)
	}
	if err != nil {
		return err
	}
	return discovery.Complete(ctx, w.runtime, run.RequestHash, result)
}
