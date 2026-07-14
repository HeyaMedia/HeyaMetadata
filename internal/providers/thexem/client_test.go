package thexem

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectUsesTVDBMapAndRequiredHeaders(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("User-Agent") != "HeyaMetadata/test" || request.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers=%v", request.Header)
		}
		if request.URL.Query().Get("id") != "378609" || request.URL.Query().Get("origin") != "tvdb" {
			t.Fatalf("query=%s", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/map/all" {
			_, _ = response.Write([]byte(`{"result":"success","data":[{"tvdb":{"season":1,"episode":1},"anidb":{"season":1,"episode":1}}]}`))
			return
		}
		if request.URL.Path != "/map/names" || request.URL.Query().Get("defaultNames") != "1" {
			t.Fatalf("unexpected request %s", request.URL.String())
		}
		_, _ = response.Write([]byte(`{"result":"success","data":{"all":"86: Eighty Six"}}`))
	}))
	t.Cleanup(server.Close)

	client := New(config.TheXEMConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tvdb", Namespace: "series", Value: "378609"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(payloads) != 2 || payloads[0].ProviderNamespace != "mapping" || payloads[1].ProviderNamespace != "names" {
		t.Fatalf("requests=%d payloads=%+v", requests, payloads)
	}
}

func TestLogicalFailureIsNotReusable(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"result":"failure","message":"no mapping"}`)}
	classifyResponse(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != 0 {
		t.Fatalf("reuse=%v", payload.ReuseDurationOverride)
	}
}
