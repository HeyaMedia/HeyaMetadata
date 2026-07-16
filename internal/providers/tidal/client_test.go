package tidal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

const artistBody = `{"data":{"id":"8847","type":"artists","attributes":{"name":"Daft Punk","popularity":0.93,` +
	`"externalLinks":[{"href":"https://tidal.com/browse/artist/8847","meta":{"type":"TIDAL_SHARING"}}]}},` +
	`"included":[{"id":"art-1","type":"artworks","attributes":{"mediaType":"IMAGE","files":[` +
	`{"href":"https://resources.tidal.com/images/480x480.jpg","meta":{"width":480,"height":480}},` +
	`{"href":"https://resources.tidal.com/images/750x750.jpg","meta":{"width":750,"height":750}}]}},` +
	`{"id":"1057","type":"artists","attributes":{"name":"Basement Jaxx","popularity":0.61,` +
	`"externalLinks":[{"href":"https://tidal.com/browse/artist/1057","meta":{"type":"TIDAL_SHARING"}}]}},` +
	`{"id":"8847","type":"artists","attributes":{"name":"Daft Punk","popularity":0.93}}]}`

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(writer http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "client-id" || password != "client-secret" {
			t.Errorf("token exchange credentials: %s %s", user, password)
		}
		if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != "client_credentials" {
			t.Errorf("token exchange form: %+v", request.PostForm)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "token-value", "expires_in": 86400})
	})
	mux.HandleFunc("/artists/8847", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token-value" {
			t.Errorf("missing bearer token: %q", request.Header.Get("Authorization"))
		}
		if request.URL.Query().Get("countryCode") != "US" || request.URL.Query().Get("include") != "profileArt,similarArtists" {
			t.Errorf("unexpected query: %s", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(artistBody))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func testConfig(server *httptest.Server) config.TidalConfig {
	return config.TidalConfig{
		ClientID: "client-id", ClientSecret: "client-secret",
		BaseURL: server.URL, AuthURL: server.URL + "/oauth2/token",
		Country: "us", RequestsPerSecond: 100,
	}
}

func TestCollectExchangesTokenAndKeepsCredentialsOutOfIdentity(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	client := New(testConfig(server))
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tidal", Namespace: "artist", Value: "8847"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads: %+v", payloads)
	}
	key := payloads[0].RequestKey
	if strings.Contains(key, "client-id") || strings.Contains(key, "client-secret") || strings.Contains(key, "token-value") {
		t.Fatalf("unsafe request key: %q", key)
	}
	if payloads[0].ProviderNamespace != "artist" || payloads[0].ProviderRecordID != "8847" {
		t.Fatalf("payload identity: %+v", payloads[0])
	}
}

func TestCollectRejectsInvalidIdentifiers(t *testing.T) {
	t.Parallel()
	client := New(config.TidalConfig{BaseURL: "https://example.invalid", AuthURL: "https://example.invalid/token", RequestsPerSecond: 100})
	for _, identifier := range []providers.Identifier{
		{Provider: "musicbrainz", Namespace: "artist", Value: "8847"},
		{Provider: "tidal", Namespace: "album", Value: "8847"},
		{Provider: "tidal", Namespace: "artist", Value: "not-numeric"},
		{Provider: "tidal", Namespace: "artist", Value: "-4"},
	} {
		if _, err := client.Collect(context.Background(), identifier); err == nil {
			t.Fatalf("expected identifier rejection: %+v", identifier)
		}
	}
}

func TestCollectRequiresCredentials(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request should not reach the network without credentials")
	}))
	defer server.Close()
	client := New(config.TidalConfig{BaseURL: server.URL, AuthURL: server.URL + "/token", RequestsPerSecond: 100})
	if _, err := client.Collect(context.Background(), providers.Identifier{Provider: "tidal", Namespace: "artist", Value: "8847"}); err == nil || !strings.Contains(err.Error(), "HEYA_METADATA_TIDAL_CLIENT_ID") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}

func TestClassifyMarksMalformedBodiesNonReusable(t *testing.T) {
	t.Parallel()
	malformed := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<html>`)}
	classify(&malformed)
	if malformed.ReuseDurationOverride == nil || *malformed.ReuseDurationOverride != 0 {
		t.Fatalf("malformed classification: %+v", malformed.ReuseDurationOverride)
	}
	valid := providers.Payload{StatusCode: http.StatusOK, Body: []byte(artistBody)}
	classify(&valid)
	if valid.ReuseDurationOverride != nil {
		t.Fatalf("valid classification should use the policy default: %+v", valid.ReuseDurationOverride)
	}
	errorStatus := providers.Payload{StatusCode: http.StatusNotFound, Body: []byte(`{"errors":[]}`)}
	classify(&errorStatus)
	if errorStatus.ReuseDurationOverride != nil {
		t.Fatalf("error statuses follow the negative-cache policy: %+v", errorStatus.ReuseDurationOverride)
	}
}

func TestNormalizeArtist(t *testing.T) {
	t.Parallel()
	record, err := NormalizeArtist([]byte(artistBody), "8847", "obs-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "tidal" || record.ProviderRecord.Value != "8847" {
		t.Fatalf("provider record: %+v", record.ProviderRecord)
	}
	if len(record.IdentityCandidates) != 1 || record.IdentityCandidates[0].Provider != "tidal" || record.IdentityCandidates[0].NormalizedValue != "8847" {
		t.Fatalf("identity candidates: %+v", record.IdentityCandidates)
	}
	if len(record.Names) != 1 || record.Names[0].Value != "Daft Punk" {
		t.Fatalf("names: %+v", record.Names)
	}
	if len(record.Metrics) != 1 || record.Metrics[0].Name != "popularity" || record.Metrics[0].Value != 0.93 {
		t.Fatalf("metrics: %+v", record.Metrics)
	}
	if len(record.Links) != 1 || record.Links[0].Type != "tidal" {
		t.Fatalf("links: %+v", record.Links)
	}
	if len(record.Images) != 1 || record.Images[0].Width != 750 || record.Images[0].Class != "profile" {
		t.Fatalf("images should keep the largest variant: %+v", record.Images)
	}
	// The subject artist echoes itself in included resources and must not
	// become its own similar artist.
	if len(record.SimilarArtists) != 1 || record.SimilarArtists[0].ProviderID != "1057" || record.SimilarArtists[0].Name != "Basement Jaxx" || record.SimilarArtists[0].URL != "https://tidal.com/browse/artist/1057" {
		t.Fatalf("similar artists: %+v", record.SimilarArtists)
	}
}

func TestNormalizeArtistRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeArtist([]byte(artistBody), "999", "obs-1", time.Now()); err == nil {
		t.Fatal("expected mismatched Tidal identity to be rejected")
	}
}
