package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/workflowfeed"
	"github.com/riverqueue/river"
)

const OutboxDrainKind = "outbox_drain_v1"

type OutboxDrainArgs struct{}

func (OutboxDrainArgs) Kind() string { return OutboxDrainKind }
func (OutboxDrainArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: BackgroundQueue, MaxAttempts: 5, Priority: PriorityScheduled, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

type OutboxDrainWorker struct {
	river.WorkerDefaults[OutboxDrainArgs]
	runtime           *platform.Runtime
	sequenceChanges   func(context.Context, *platform.Runtime, int) error
	sequenceWorkflows func(context.Context, *platform.Runtime, int) error
	pending           func(context.Context) (bool, bool, error)
	sequenceTimeout   time.Duration
}

func NewOutboxDrainWorker(runtime *platform.Runtime) *OutboxDrainWorker {
	return &OutboxDrainWorker{
		runtime:           runtime,
		sequenceChanges:   changelog.Sequence,
		sequenceWorkflows: workflowfeed.Sequence,
		sequenceTimeout:   30 * time.Second,
		pending: func(ctx context.Context) (bool, bool, error) {
			var changesPending, workflowsPending bool
			err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM change_outbox WHERE sequenced_at IS NULL),EXISTS(SELECT 1 FROM workflow_event_outbox WHERE sequenced_at IS NULL)`).Scan(&changesPending, &workflowsPending)
			return changesPending, workflowsPending, err
		},
	}
}

func (w *OutboxDrainWorker) Timeout(*river.Job[OutboxDrainArgs]) time.Duration {
	return 2 * time.Minute
}

func (w *OutboxDrainWorker) Work(ctx context.Context, _ *river.Job[OutboxDrainArgs]) error {
	const batchSize = 500
	const maxBatches = 100
	for batch := 0; batch < maxBatches; batch++ {
		changeErr, workflowErr := w.sequenceBatch(ctx, batchSize)
		if changeErr != nil || workflowErr != nil {
			var failures []error
			if changeErr != nil {
				failures = append(failures, fmt.Errorf("drain change outbox: %w", changeErr))
			}
			if workflowErr != nil {
				failures = append(failures, fmt.Errorf("drain workflow outbox: %w", workflowErr))
			}
			return errors.Join(failures...)
		}
		changesPending, workflowsPending, err := w.pending(ctx)
		if err != nil {
			return fmt.Errorf("check outbox backlog: %w", err)
		}
		if !changesPending && !workflowsPending {
			return nil
		}
	}
	slog.WarnContext(ctx, "outbox drain reached bounded batch limit", "batch_size", batchSize, "max_batches", maxBatches)
	return nil
}

// sequenceBatch starts both independent feeds before waiting for either. Each
// gets its own bounded context so a blocked database/feed path cannot consume
// the worker's full timeout before the other sequencer is even attempted.
func (w *OutboxDrainWorker) sequenceBatch(ctx context.Context, batchSize int) (error, error) {
	timeout := w.sequenceTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	changeCtx, cancelChange := context.WithTimeout(ctx, timeout)
	workflowCtx, cancelWorkflow := context.WithTimeout(ctx, timeout)
	defer cancelChange()
	defer cancelWorkflow()
	changeResult := make(chan error, 1)
	workflowResult := make(chan error, 1)
	go func() { changeResult <- w.sequenceChanges(changeCtx, w.runtime, batchSize) }()
	go func() { workflowResult <- w.sequenceWorkflows(workflowCtx, w.runtime, batchSize) }()
	changeErr := waitSequenceResult(changeCtx, changeResult)
	workflowErr := waitSequenceResult(workflowCtx, workflowResult)
	return changeErr, workflowErr
}

// waitSequenceResult gives a result that is already buffered priority over an
// expired child context. This matters because sequenceBatch waits for the two
// concurrently-started feeds in a stable order: the second feed can finish
// successfully while the first one consumes the shared timeout, leaving both
// its result channel and deadline ready when we get here.
func waitSequenceResult(child context.Context, result <-chan error) error {
	select {
	case err := <-result:
		return err
	default:
	}

	select {
	case err := <-result:
		return err
	case <-child.Done():
		// Resolve the boundary race in favor of completed work. Without this
		// second drain, select may report a deadline even though the result was
		// published concurrently with cancellation.
		select {
		case err := <-result:
			return err
		default:
			return child.Err()
		}
	}
}
