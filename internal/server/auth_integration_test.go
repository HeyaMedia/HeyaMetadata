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
