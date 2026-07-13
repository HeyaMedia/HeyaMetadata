package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/auth"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
)

type authRegisterInput struct {
	Body struct {
		Username string `json:"username" minLength:"3" maxLength:"64" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{1,62}[A-Za-z0-9]$"`
		Password string `json:"password" minLength:"10" maxLength:"128" format:"password" writeOnly:"true"`
	}
}

type authLoginInput struct {
	Body struct {
		Username string `json:"username" minLength:"3" maxLength:"64"`
		Password string `json:"password" minLength:"1" maxLength:"128" format:"password" writeOnly:"true"`
	}
}

type authSessionInput struct {
	Session string `cookie:"__Host-heya_session" doc:"Opaque browser session; set and read only by the browser"`
}

type authBody struct {
	User auth.User `json:"user"`
}

type authSessionOutput struct {
	SetCookie    http.Cookie `header:"Set-Cookie"`
	CacheControl string      `header:"Cache-Control"`
	Body         authBody
}

type authMeOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         authBody
}

type authLogoutOutput struct {
	SetCookie    http.Cookie `header:"Set-Cookie"`
	CacheControl string      `header:"Cache-Control"`
}

func registerAuth(api huma.API, runtime *platform.Runtime) {
	var service *auth.Service
	if runtime != nil {
		service = auth.New(runtime.DB, runtime.Redis)
	}

	huma.Register(api, huma.Operation{
		OperationID:   "auth-register",
		Method:        http.MethodPost,
		Path:          "/api/v2/auth/register",
		Summary:       "Create a local user account",
		Description:   "Creates a UUID-backed user, hashes the password with Argon2id, and starts an opaque browser session.",
		Tags:          []string{"Authentication"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  4096,
		Errors:        []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *authRegisterInput) (*authSessionOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, token, err := service.Register(ctx, input.Body.Username, input.Body.Password)
		if err != nil {
			return nil, authHTTPError(ctx, "register user", err)
		}
		return &authSessionOutput{
			SetCookie:    newSessionCookie(token, time.Now()),
			CacheControl: "no-store",
			Body:         authBody{User: user},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:  "auth-login",
		Method:       http.MethodPost,
		Path:         "/api/v2/auth/login",
		Summary:      "Sign in with a local account",
		Tags:         []string{"Authentication"},
		MaxBodyBytes: 4096,
		Errors:       []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *authLoginInput) (*authSessionOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, token, err := service.Login(ctx, input.Body.Username, input.Body.Password)
		if err != nil {
			return nil, authHTTPError(ctx, "log in user", err)
		}
		return &authSessionOutput{
			SetCookie:    newSessionCookie(token, time.Now()),
			CacheControl: "no-store",
			Body:         authBody{User: user},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/api/v2/auth/me",
		Summary:     "Get the signed-in user",
		Tags:        []string{"Authentication"},
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *authSessionInput) (*authMeOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, err := service.CurrentUser(ctx, input.Session)
		if err != nil {
			return nil, authHTTPError(ctx, "load current user", err)
		}
		return &authMeOutput{CacheControl: "no-store", Body: authBody{User: user}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "auth-logout",
		Method:        http.MethodPost,
		Path:          "/api/v2/auth/logout",
		Summary:       "End the current browser session",
		Tags:          []string{"Authentication"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *authSessionInput) (*authLogoutOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		if err := service.Logout(ctx, input.Session); err != nil {
			return nil, authHTTPError(ctx, "log out user", err)
		}
		return &authLogoutOutput{
			SetCookie:    expiredSessionCookie(),
			CacheControl: "no-store",
		}, nil
	})
}

func authHTTPError(ctx context.Context, operation string, err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrInvalidPassword):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, auth.ErrUsernameTaken):
		return huma.Error409Conflict("username is already taken")
	case errors.Is(err, auth.ErrInvalidCredential):
		return huma.Error401Unauthorized("invalid username or password")
	case errors.Is(err, auth.ErrUnauthenticated):
		return huma.Error401Unauthorized("authentication required")
	default:
		slog.ErrorContext(ctx, "authentication operation failed", "operation", operation, "error", err)
		return huma.Error503ServiceUnavailable("authentication service is unavailable")
	}
}

func newSessionCookie(token string, now time.Time) http.Cookie {
	return http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  now.Add(auth.SessionTTL).UTC(),
		MaxAge:   int(auth.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

func expiredSessionCookie() http.Cookie {
	return http.Cookie{
		Name:     auth.SessionCookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}
