package openopus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestComposerCollectionIncludesIdentityAndWorks(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/composer/list/ids/") {
			_, _ = writer.Write([]byte(`{"status":{"success":"true","rows":1},"composers":[{"id":"145"}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"status":{"success":"true","rows":700},"composer":{"id":"145"},"works":[]}`))
	}))
	defer server.Close()
	client := New(config.OpenOpusConfig{BaseURL: server.URL, RequestsPerSecond: 1000})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "openopus", Namespace: "composer", Value: "145"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || payloads[1].ProviderNamespace != "composer_works" {
		t.Fatalf("payloads: %+v", payloads)
	}
}

func TestLogicalNoMatchUsesNegativeReuse(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"status":{"success":"false","rows":0}}`)}
	classify(24 * time.Hour)(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Hour {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}
