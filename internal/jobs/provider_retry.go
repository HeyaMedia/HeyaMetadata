package jobs

import (
	"errors"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/riverqueue/river"
)

const defaultProviderRateLimitCooldown = 2 * time.Minute
const aniDBBanCooldown = 30 * time.Minute

func providerRateLimitSnooze(err error) (error, bool) {
	var status *providers.StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusTooManyRequests {
		return nil, false
	}
	delay := defaultProviderRateLimitCooldown
	if status.Provider == "anidb" {
		delay = aniDBBanCooldown
	}
	return river.JobSnooze(delay), true
}
