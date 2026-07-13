package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/riverqueue/river"
)

const (
	ImageMaterializeKind = "image_materialize_v1"
	ImageMaintenanceKind = "image_maintenance_v1"
	ImageQueue           = "images"
)

type ImageMaterializeArgs struct {
	ImageID string `json:"image_id" river:"unique"`
}

func (ImageMaterializeArgs) Kind() string { return ImageMaterializeKind }
func (ImageMaterializeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ImageQueue, MaxAttempts: 4, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

type ImageMaintenanceArgs struct{}

func (ImageMaintenanceArgs) Kind() string { return ImageMaintenanceKind }
func (ImageMaintenanceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: BackgroundQueue, Priority: PriorityScheduled, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

type ImageMaintenanceWorker struct {
	river.WorkerDefaults[ImageMaintenanceArgs]
	runtime *platform.Runtime
}

func NewImageMaintenanceWorker(runtime *platform.Runtime) *ImageMaintenanceWorker {
	return &ImageMaintenanceWorker{runtime: runtime}
}

func (w *ImageMaintenanceWorker) Work(ctx context.Context, _ *river.Job[ImageMaintenanceArgs]) error {
	if _, err := images.RecoverStalled(ctx, w.runtime, 30*time.Minute); err != nil {
		return err
	}
	if _, err := images.FlushAccesses(ctx, w.runtime, 5000); err != nil {
		return err
	}
	if _, err := images.SweepCold(ctx, w.runtime, 500, images.ColdAfter); err != nil {
		return err
	}
	_, err := images.SweepOrphans(ctx, w.runtime, 500, images.OrphanGrace)
	return err
}

type ImageMaterializeWorker struct {
	river.WorkerDefaults[ImageMaterializeArgs]
	service *images.Service
}

func NewImageMaterializeWorker(runtime *platform.Runtime) *ImageMaterializeWorker {
	return &ImageMaterializeWorker{service: images.NewService(runtime)}
}
func (w *ImageMaterializeWorker) Work(ctx context.Context, job *river.Job[ImageMaterializeArgs]) error {
	_, err := w.service.Materialize(ctx, job.Args.ImageID)
	if errors.Is(err, images.ErrNotFound) {
		return river.JobCancel(err)
	}
	if errors.Is(err, images.ErrInProgress) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("materialize image %s: %w", job.Args.ImageID, err)
	}
	return nil
}
