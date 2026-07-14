package jobs

import (
	"errors"
	"net/http"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/riverqueue/river"
)

func TestProviderRateLimitSnoozeUsesAniDBBanCooldown(t *testing.T) {
	t.Parallel()
	err, ok := providerRateLimitSnooze(&providers.StatusError{Provider: "anidb", StatusCode: http.StatusTooManyRequests})
	if !ok {
		t.Fatal("AniDB rate limit was not classified")
	}
	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration != aniDBBanCooldown {
		t.Fatalf("snooze: %+v", snooze)
	}
}
