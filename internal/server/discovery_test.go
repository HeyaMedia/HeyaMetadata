package server

import (
	"net/http"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
)

func TestFailedDiscoveryIsRetryableServiceFailure(t *testing.T) {
	t.Parallel()
	output := discoveryRunOutput(discovery.Run{ID: "2a059429-ca46-44b7-a9b5-b5b54e47a889", State: "failed", Error: "upstream unavailable"})
	if output.Status != http.StatusServiceUnavailable || output.RetryAfter != "5" {
		t.Fatalf("status=%d retry_after=%q", output.Status, output.RetryAfter)
	}
	if output.Body.State != "failed" || output.Body.Error != "upstream unavailable" {
		t.Fatalf("body=%+v", output.Body)
	}
}

func TestNonfailedDiscoveryUsesSuccessfulStatus(t *testing.T) {
	t.Parallel()
	output := discoveryRunOutput(discovery.Run{State: "working"})
	if output.Status != http.StatusOK || output.RetryAfter != "" {
		t.Fatalf("status=%d retry_after=%q", output.Status, output.RetryAfter)
	}
}
