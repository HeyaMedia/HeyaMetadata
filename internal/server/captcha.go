package server

import (
	"context"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/auth"
	"github.com/danielgtaylor/huma/v2"
)

type captchaChallengeOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         auth.Challenge
}

func registerCaptcha(api huma.API, captcha *auth.Captcha) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-challenge",
		Method:      http.MethodGet,
		Path:        "/api/v2/auth/challenge",
		Summary:     "Issue a proof-of-work captcha challenge",
		Description: "Returns an Altcha-style proof-of-work challenge the client solves before register/login. Returns 404 when the captcha is disabled.",
		Tags:        []string{"Authentication"},
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*captchaChallengeOutput, error) {
		if captcha == nil {
			return nil, huma.Error404NotFound("captcha is not enabled")
		}
		challenge, err := captcha.CreateChallenge(time.Now())
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("captcha is unavailable")
		}
		return &captchaChallengeOutput{CacheControl: "no-store", Body: challenge}, nil
	})
}

// verifyCaptcha checks the solution when the captcha is enabled; a nil verifier
// (disabled) passes through so register/login work without it.
func verifyCaptcha(ctx context.Context, captcha *auth.Captcha, solution string) error {
	if captcha == nil {
		return nil
	}
	return captcha.Verify(ctx, solution, time.Now())
}
