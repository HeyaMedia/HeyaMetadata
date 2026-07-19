package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func TestOutboxDrainAttemptsWorkflowWhenChangeSequencingFails(t *testing.T) {
	changeFailure := errors.New("change feed unavailable")
	workflowCalls := 0
	worker := &OutboxDrainWorker{
		sequenceChanges: func(context.Context, *platform.Runtime, int) error {
			return changeFailure
		},
		sequenceWorkflows: func(context.Context, *platform.Runtime, int) error {
			workflowCalls++
			return nil
		},
		pending: func(context.Context) (bool, bool, error) {
			t.Fatal("backlog should not be checked after a sequencing failure")
			return false, false, nil
		},
	}
	err := worker.Work(t.Context(), nil)
	if !errors.Is(err, changeFailure) || workflowCalls != 1 {
		t.Fatalf("err=%v workflow calls=%d", err, workflowCalls)
	}
	if !strings.Contains(err.Error(), "drain change outbox") {
		t.Fatalf("sequencer error lost its operation: %v", err)
	}
}

func TestOutboxDrainStartsWorkflowWhileChangeSequencerIsBlocked(t *testing.T) {
	changeStarted := make(chan struct{})
	workflowStarted := make(chan struct{})
	releaseChange := make(chan struct{})
	defer close(releaseChange)
	worker := &OutboxDrainWorker{
		sequenceTimeout: 25 * time.Millisecond,
		sequenceChanges: func(context.Context, *platform.Runtime, int) error {
			close(changeStarted)
			<-releaseChange
			return nil
		},
		sequenceWorkflows: func(context.Context, *platform.Runtime, int) error {
			close(workflowStarted)
			return nil
		},
		pending: func(context.Context) (bool, bool, error) { return false, false, nil },
	}
	changeErr, workflowErr := worker.sequenceBatch(t.Context(), 500)
	if !errors.Is(changeErr, context.DeadlineExceeded) {
		t.Fatalf("blocked change sequencer error=%v", changeErr)
	}
	if workflowErr != nil {
		t.Fatalf("completed workflow sequencer was mistaken for a timeout: %v", workflowErr)
	}
	select {
	case <-changeStarted:
	default:
		t.Fatal("change sequencer was not started")
	}
	select {
	case <-workflowStarted:
	default:
		t.Fatal("blocked change sequencer starved workflow sequencer")
	}
}

func TestWaitSequenceResultPrefersBufferedCompletionAtDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	result <- nil
	cancel()

	if err := waitSequenceResult(ctx, result); err != nil {
		t.Fatalf("buffered completion lost to ready deadline: %v", err)
	}
}

func TestOutboxDrainReportsBothSequencerFailures(t *testing.T) {
	changeFailure := errors.New("change failed")
	workflowFailure := errors.New("workflow failed")
	worker := &OutboxDrainWorker{
		sequenceChanges:   func(context.Context, *platform.Runtime, int) error { return changeFailure },
		sequenceWorkflows: func(context.Context, *platform.Runtime, int) error { return workflowFailure },
		pending:           func(context.Context) (bool, bool, error) { return false, false, nil },
	}
	err := worker.Work(t.Context(), nil)
	if !errors.Is(err, changeFailure) || !errors.Is(err, workflowFailure) {
		t.Fatalf("joined sequencing error=%v", err)
	}
}
