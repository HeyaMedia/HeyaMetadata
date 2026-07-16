package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

// PromoteOperatorJob makes explicit operator work the next eligible job in its
// queue without interrupting a running job or bypassing a retry backoff. River
// sorts available work by priority, scheduled_at, and id; priority alone is
// insufficient when the queue already contains older interactive work.
func PromoteOperatorJob(ctx context.Context, runtime *platform.Runtime, jobID int64) error {
	_, err := runtime.DB.Exec(ctx, `
		UPDATE river_job
		SET priority=$2,
			state=CASE WHEN state='scheduled' THEN 'available' ELSE state END,
			scheduled_at=CASE WHEN state IN('available','scheduled') THEN to_timestamp(0) ELSE scheduled_at END
		WHERE id=$1 AND state IN('available','retryable','scheduled')`, jobID, PriorityInteractive)
	if err != nil {
		return fmt.Errorf("promote operator job %d: %w", jobID, err)
	}
	return nil
}
