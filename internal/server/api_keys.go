package server

import (
	"context"
	"net/http"

	"github.com/HeyaMedia/HeyaMetadata/internal/auth"
	"github.com/danielgtaylor/huma/v2"
)

type apiKeyCreateInput struct {
	Session string `cookie:"__Host-heya_session" doc:"Opaque browser session; set and read only by the browser"`
	Body    struct {
		Name string `json:"name" minLength:"1" maxLength:"64" doc:"Human-readable name for the client or device"`
	}
}

type apiKeyListInput struct {
	Session string `cookie:"__Host-heya_session" doc:"Opaque browser session; set and read only by the browser"`
}

type apiKeyDeleteInput struct {
	Session string `cookie:"__Host-heya_session" doc:"Opaque browser session; set and read only by the browser"`
	ID      string `path:"id" format:"uuid"`
}

type apiKeyCreateOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		APIKey auth.CreatedAPIKey `json:"api_key"`
	}
}

type apiKeyListOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		APIKeys []auth.APIKey `json:"api_keys"`
	}
}

type apiKeyDeleteOutput struct {
	CacheControl string `header:"Cache-Control"`
}

func registerAPIKeys(api huma.API, service *auth.Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-api-key",
		Method:        http.MethodPost,
		Path:          "/api/v2/auth/api-keys",
		Summary:       "Create a user API key",
		Description:   "Returns the plaintext API key once. Only a non-reversible digest is retained afterward.",
		Tags:          []string{"Authentication"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  4096,
		Errors:        []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *apiKeyCreateInput) (*apiKeyCreateOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, err := service.CurrentUser(ctx, input.Session)
		if err != nil {
			return nil, authHTTPError(ctx, "authenticate API key creation", err)
		}
		created, err := service.CreateAPIKey(ctx, user.ID, input.Body.Name)
		if err != nil {
			return nil, authHTTPError(ctx, "create API key", err)
		}
		output := &apiKeyCreateOutput{CacheControl: "no-store"}
		output.Body.APIKey = created
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-api-keys",
		Method:      http.MethodGet,
		Path:        "/api/v2/auth/api-keys",
		Summary:     "List the current user's API keys",
		Description: "Lists active key metadata. Plaintext API keys are never returned after creation.",
		Tags:        []string{"Authentication"},
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *apiKeyListInput) (*apiKeyListOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, err := service.CurrentUser(ctx, input.Session)
		if err != nil {
			return nil, authHTTPError(ctx, "authenticate API key listing", err)
		}
		keys, err := service.ListAPIKeys(ctx, user.ID)
		if err != nil {
			return nil, authHTTPError(ctx, "list API keys", err)
		}
		output := &apiKeyListOutput{CacheControl: "no-store"}
		output.Body.APIKeys = keys
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-api-key",
		Method:        http.MethodDelete,
		Path:          "/api/v2/auth/api-keys/{id}",
		Summary:       "Revoke a user API key",
		Tags:          []string{"Authentication"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *apiKeyDeleteInput) (*apiKeyDeleteOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("authentication service is unavailable")
		}
		user, err := service.CurrentUser(ctx, input.Session)
		if err != nil {
			return nil, authHTTPError(ctx, "authenticate API key revocation", err)
		}
		if err := service.RevokeAPIKey(ctx, user.ID, input.ID); err != nil {
			return nil, authHTTPError(ctx, "revoke API key", err)
		}
		return &apiKeyDeleteOutput{CacheControl: "no-store"}, nil
	})
}
