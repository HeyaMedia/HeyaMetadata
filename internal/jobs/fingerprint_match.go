package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprintmatch"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const FingerprintMatchKind = "fingerprint_match_v1"

type FingerprintMatchArgs struct {
	RunID         string `json:"run_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

func (FingerprintMatchArgs) Kind() string { return FingerprintMatchKind }
func (FingerprintMatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertFingerprintMatch(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args FingerprintMatchArgs) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, nil)
	if err != nil {
		return nil, err
	}
	if err = fingerprintmatch.AttachJob(ctx, runtime, args.RunID, inserted.Job.ID); err != nil {
		return nil, err
	}
	return inserted, nil
}

type FingerprintMatchWorker struct {
	river.WorkerDefaults[FingerprintMatchArgs]
	runtime *platform.Runtime
	service *fingerprintmatch.Service
}

func NewFingerprintMatchWorker(r *platform.Runtime) *FingerprintMatchWorker {
	return &FingerprintMatchWorker{runtime: r, service: fingerprintmatch.NewService(r)}
}
func (w *FingerprintMatchWorker) Work(ctx context.Context, job *river.Job[FingerprintMatchArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.MatchRun(ctx, job.Args.RunID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
		return nil
	}
	return fmt.Errorf("match recording fingerprint: %w", err)
}
