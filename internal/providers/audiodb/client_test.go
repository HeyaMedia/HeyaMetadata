package audiodb

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

const weekndMBID = "c8b03190-306c-4120-bb0b-6f2ebfc06ea9"

func TestCollectKeepsAPIKeyOutOfRequestIdentity(t *testing.T) {
	t.Parallel()
	const apiKey = "path-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/"+apiKey+"/artist-mb.php" || request.URL.Query().Get("i") != weekndMBID {
			t.Errorf("unexpected TheAudioDB request: %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`{"artists":[{"idArtist":"112024","strArtist":"The Weeknd"}]}`))
	}))
	defer server.Close()
	client := New(config.AudioDBConfig{BaseURL: server.URL, APIKey: apiKey, RequestsPerSecond: 100})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: weekndMBID})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0].RequestKey, apiKey) {
		t.Fatalf("unsafe payload identity: %+v", payloads)
	}
	if payloads[0].ProviderNamespace != "artist" || payloads[0].ProviderRecordID != weekndMBID || payloads[0].RequestKey != "artist-mb/"+weekndMBID {
		t.Fatalf("payload identity: %+v", payloads[0])
	}
}

func TestCollectRejectsNonMusicBrainzIdentifiers(t *testing.T) {
	t.Parallel()
	client := New(config.AudioDBConfig{BaseURL: "https://example.invalid", APIKey: "2", RequestsPerSecond: 100})
	for _, identifier := range []providers.Identifier{
		{Provider: "deezer", Namespace: "artist", Value: "27"},
		{Provider: "musicbrainz", Namespace: "release", Value: weekndMBID},
		{Provider: "musicbrainz", Namespace: "artist", Value: "not-a-mbid"},
	} {
		if _, err := client.Collect(context.Background(), identifier); err == nil {
			t.Fatalf("expected identifier rejection: %+v", identifier)
		}
	}
}

func TestCollectRequiresAPIKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request should not reach the network without an API key")
	}))
	defer server.Close()
	client := New(config.AudioDBConfig{BaseURL: server.URL, RequestsPerSecond: 100})
	if _, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: weekndMBID}); err == nil || !strings.Contains(err.Error(), "HEYA_METADATA_AUDIODB_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestClassifyMarksEmptyAndMalformedResponses(t *testing.T) {
	t.Parallel()
	empty := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"artists":null}`)}
	classify(&empty)
	if empty.ReuseDurationOverride == nil || *empty.ReuseDurationOverride != time.Hour {
		t.Fatalf("empty classification: %+v", empty.ReuseDurationOverride)
	}
	malformed := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<html>`)}
	classify(&malformed)
	if malformed.ReuseDurationOverride == nil || *malformed.ReuseDurationOverride != 0 {
		t.Fatalf("malformed classification: %+v", malformed.ReuseDurationOverride)
	}
	found := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"artists":[{"strArtist":"The Weeknd"}]}`)}
	classify(&found)
	if found.ReuseDurationOverride != nil {
		t.Fatalf("found classification should use the policy default: %+v", found.ReuseDurationOverride)
	}
}
