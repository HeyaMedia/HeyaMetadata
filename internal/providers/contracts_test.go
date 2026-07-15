package providers

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestHasHTTPStatusRecognizesWrappedProviderFailures(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("collect supplement: %w", &StatusError{Provider: "fanart", StatusCode: http.StatusNotFound})
	if !HasHTTPStatus(err, http.StatusNotFound) {
		t.Fatal("wrapped provider status was not recognized")
	}
	if HasHTTPStatus(err, http.StatusTooManyRequests) {
		t.Fatal("provider status matched the wrong code")
	}
}

func TestRequestFingerprintSeparatesProviders(t *testing.T) {
	t.Parallel()
	if RequestFingerprint("tmdb", "movie/603") == RequestFingerprint("tvdb", "movie/603") {
		t.Fatal("provider must be part of request fingerprint")
	}
	if RequestFingerprint("tmdb", "movie/603") != RequestFingerprint("tmdb", "movie/603") {
		t.Fatal("request fingerprint is not stable")
	}
}

func TestResponseCachePolicyStatusDurations(t *testing.T) {
	t.Parallel()
	policy := ResponseCachePolicy{ReuseDuration: 48 * time.Hour, NegativeDuration: time.Hour}
	if got := policy.DurationForStatus(http.StatusOK); got != 48*time.Hour {
		t.Fatalf("success duration: %s", got)
	}
	if got := policy.DurationForStatus(http.StatusNotFound); got != time.Hour {
		t.Fatalf("negative duration: %s", got)
	}
	if got := policy.DurationForStatus(http.StatusUnauthorized); got != 0 {
		t.Fatalf("credentials failure must not be reusable: %s", got)
	}
}

func TestPayloadReuseOverrideHandlesLogicalErrorsInsideHTTP200(t *testing.T) {
	t.Parallel()
	policy := ResponseCachePolicy{ReuseDuration: 24 * time.Hour}
	zero := time.Duration(0)
	payload := Payload{StatusCode: http.StatusOK, ReuseDurationOverride: &zero}
	if got := policy.DurationForPayload(payload); got != 0 {
		t.Fatalf("logical provider error was reusable for %s", got)
	}
}
