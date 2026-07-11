package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/riverqueue/river"
)

const ImageMaterializeKind = "image_materialize_v1"

type ImageMaterializeArgs struct {
	ImageID string `json:"image_id" river:"unique"`
}

func (ImageMaterializeArgs) Kind() string { return ImageMaterializeKind }
func (ImageMaterializeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 4, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
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
