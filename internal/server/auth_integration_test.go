package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/auth"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/migrations"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestAuthHTTPRoundTrip(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres and Redis stack")
	}
	if err := config.LoadEnvFiles(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	if _, err := migrations.MigrateApp(ctx, database); err != nil {
		t.Fatal(err)
	}

	runtime := &platform.Runtime{DB: database, Redis: redisClient, Config: cfg}
	handler := NewWithRuntime("integration", runtime).Handler()
	username := "http_auth_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "_")
	password := "integration-password"

	register := serveJSON(t, handler, http.MethodPost, "/api/v2/auth/register", map[string]string{"username": username, "password": password}, nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("register status: got %d body=%s", register.Code, register.Body.String())
	}
	var registered authBody
	if err := json.NewDecoder(register.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	cookie := responseCookie(t, register)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = database.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, registered.User.ID)
	})

	me := serveJSON(t, handler, http.MethodGet, "/api/v2/auth/me", nil, cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me status: got %d body=%s", me.Code, me.Body.String())
	}
	var current authBody
	if err := json.NewDecoder(me.Body).Decode(&current); err != nil {
		t.Fatal(err)
	}
	if current.User.ID != registered.User.ID || current.User.Username != username || current.User.Role != "user" {
		t.Fatalf("unexpected current user: %+v", current.User)
	}

	refreshPath := "/api/v2/entities/00000000-0000-4000-8000-000000000001/refreshes"
	if response := serveJSON(t, handler, http.MethodPost, refreshPath, nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous refresh status: got %d body=%s", response.Code, response.Body.String())
	}
	if response := serveJSON(t, handler, http.MethodPost, refreshPath, nil, cookie); response.Code != http.StatusForbidden {
		t.Fatalf("ordinary user refresh status: got %d body=%s", response.Code, response.Body.String())
	}
	if _, err := database.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, registered.User.ID); err != nil {
		t.Fatal(err)
	}
	// A 404 proves the admin passed authorization and reached entity lookup;
	// the synthetic UUID deliberately does not name a canonical entity.
	if response := serveJSON(t, handler, http.MethodPost, refreshPath, nil, cookie); response.Code != http.StatusNotFound {
		t.Fatalf("admin refresh status: got %d body=%s", response.Code, response.Body.String())
	}

	createKey := serveJSON(t, handler, http.MethodPost, "/api/v2/auth/api-keys", map[string]string{"name": "Living Room"}, cookie)
	if createKey.Code != http.StatusCreated {
		t.Fatalf("create API key status: got %d body=%s", createKey.Code, createKey.Body.String())
	}
	var createdKey struct {
		APIKey auth.CreatedAPIKey `json:"api_key"`
	}
	if err := json.NewDecoder(createKey.Body).Decode(&createdKey); err != nil {
		t.Fatal(err)
	}
	if createdKey.APIKey.Key == "" || createdKey.APIKey.Prefix == "" || createdKey.APIKey.Name != "Living Room" {
		t.Fatalf("unexpected API key creation body: %+v", createdKey.APIKey)
	}

	listKeys := serveJSON(t, handler, http.MethodGet, "/api/v2/auth/api-keys", nil, cookie)
	if listKeys.Code != http.StatusOK {
		t.Fatalf("list API keys status: got %d body=%s", listKeys.Code, listKeys.Body.String())
	}
	listBody := listKeys.Body.Bytes()
	if bytes.Contains(listBody, []byte(createdKey.APIKey.Key)) || bytes.Contains(listBody, []byte(`"key"`)) {
		t.Fatalf("API key listing exposed plaintext: %s", listBody)
	}
	var listedKeys struct {
		APIKeys []auth.APIKey `json:"api_keys"`
	}
	if err := json.Unmarshal(listBody, &listedKeys); err != nil {
		t.Fatal(err)
	}
	if len(listedKeys.APIKeys) != 1 || listedKeys.APIKeys[0].ID != createdKey.APIKey.ID {
		t.Fatalf("unexpected API key listing: %+v", listedKeys.APIKeys)
	}

	keyMe := serveBearer(t, handler, "/api/v2/auth/me", createdKey.APIKey.Key)
	if keyMe.Code != http.StatusOK {
		t.Fatalf("API key authentication: got %d body=%s", keyMe.Code, keyMe.Body.String())
	}
	revokeKey := serveJSON(t, handler, http.MethodDelete, "/api/v2/auth/api-keys/"+createdKey.APIKey.ID, nil, cookie)
	if revokeKey.Code != http.StatusNoContent {
		t.Fatalf("revoke API key status: got %d body=%s", revokeKey.Code, revokeKey.Body.String())
	}
	if revokedMe := serveBearer(t, handler, "/api/v2/auth/me", createdKey.APIKey.Key); revokedMe.Code != http.StatusUnauthorized {
		t.Fatalf("revoked API key authentication: got %d body=%s", revokedMe.Code, revokedMe.Body.String())
	}

	logout := serveJSON(t, handler, http.MethodPost, "/api/v2/auth/logout", nil, cookie)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status: got %d body=%s", logout.Code, logout.Body.String())
	}
	if cleared := responseCookie(t, logout); cleared.MaxAge >= 0 {
		t.Fatalf("logout did not expire cookie: %+v", cleared)
	}
	if afterLogout := serveJSON(t, handler, http.MethodGet, "/api/v2/auth/me", nil, cookie); afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: got %d body=%s", afterLogout.Code, afterLogout.Body.String())
	}

	login := serveJSON(t, handler, http.MethodPost, "/api/v2/auth/login", map[string]string{"username": username, "password": password}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status: got %d body=%s", login.Code, login.Body.String())
	}
	loginCookie := responseCookie(t, login)
	finalLogout := serveJSON(t, handler, http.MethodPost, "/api/v2/auth/logout", nil, loginCookie)
	if finalLogout.Code != http.StatusNoContent {
		t.Fatalf("final logout status: got %d body=%s", finalLogout.Code, finalLogout.Body.String())
	}
}

func serveBearer(t *testing.T, handler http.Handler, path, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func serveJSON(t *testing.T, handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &encoded)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.SessionCookieName {
			return cookie
		}
	}
	t.Fatalf("response did not contain %s cookie", auth.SessionCookieName)
	return nil
}
