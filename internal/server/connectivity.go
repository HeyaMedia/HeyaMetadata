package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/connectivity"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/redis/go-redis/v9"
)

const (
	connectivityRateWindow = time.Minute
	connectivityLockTTL    = 15 * time.Second
)

var connectivityChallengePattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

type requestContextKey struct{}

type requestDetails struct {
	RemoteAddr string
	Header     http.Header
}

func captureRequestDetails(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		details := requestDetails{RemoteAddr: request.RemoteAddr, Header: request.Header.Clone()}
		ctx := context.WithValue(request.Context(), requestContextKey{}, details)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func requestDetailsFromContext(ctx context.Context) (*http.Request, bool) {
	details, ok := ctx.Value(requestContextKey{}).(requestDetails)
	if !ok {
		return nil, false
	}
	return &http.Request{RemoteAddr: details.RemoteAddr, Header: details.Header}, true
}

type connectivityIPResult struct {
	IP string `json:"ip"`
}

type connectivityRateLimitResponse struct {
	RetryAfterSeconds int `json:"retry_after_seconds" minimum:"1"`
}

// connectivityCheckResult exists as the OpenAPI model for the raw JSON body.
// TLS and Error are optional in the generated Go client so JSON null decodes
// naturally; runtime responses always include both keys as required by the
// media-server contract.
type connectivityCheckResult struct {
	ObservedIP string                   `json:"observed_ip"`
	Reachable  bool                     `json:"reachable"`
	Verified   bool                     `json:"verified"`
	LatencyMS  int64                    `json:"latency_ms"`
	TLS        *connectivity.TLSInfo    `json:"tls,omitempty"`
	Error      *connectivity.ProbeError `json:"error,omitempty"`
}

type connectivityIPOutput struct {
	Status     int    `json:"-"`
	RetryAfter string `header:"Retry-After"`
	Body       json.RawMessage
}

type connectivityCheckRequest struct {
	Port      int    `json:"port"`
	Challenge string `json:"challenge"`
}

type connectivityCheckInput struct {
	Body connectivityCheckRequest
}

type connectivityCheckOutput struct {
	Status     int    `json:"-"`
	RetryAfter string `header:"Retry-After"`
	Body       json.RawMessage
}

type connectivityProbeRunner interface {
	Probe(context.Context, netip.Addr, int, string) connectivity.Result
}

func registerConnectivity(api huma.API, runtime *platform.Runtime, resolver *connectivity.ClientIPResolver) {
	registerConnectivityService(api, resolver, connectivity.NewLimiter(runtimeRedis(runtime)), connectivity.NewProber())
}

func registerConnectivityService(api huma.API, resolver *connectivity.ClientIPResolver, limiter *connectivity.Limiter, prober connectivityProbeRunner) {
	registerConnectivitySchemas(api)
	huma.Register(api, huma.Operation{
		OperationID:   "connectivity-ip",
		Method:        http.MethodGet,
		Path:          "/v1/ip",
		Summary:       "Return the caller's public source IP",
		Description:   "Returns the source address observed by heya.media after trusted-proxy resolution. Private and reserved addresses are rejected.",
		Tags:          []string{"Connectivity"},
		DefaultStatus: http.StatusOK,
		Responses: map[string]*huma.Response{
			"200": jsonResponse("Observed public source IP", "#/components/schemas/ConnectivityIPResult"),
			"429": withRetryAfter(jsonResponse("Source IP rate limit exceeded", "#/components/schemas/ConnectivityRateLimitResponse")),
		},
	}, func(ctx context.Context, _ *struct{}) (*connectivityIPOutput, error) {
		address, invalid := resolveConnectivityAddress(ctx, resolver)
		if invalid != nil {
			return nil, invalid
		}
		allowed, retry, err := limiter.Allow(ctx, "ip", address.String(), 60, connectivityRateWindow)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("connectivity rate limiter is unavailable", err)
		}
		if !allowed {
			retry = max(retry, 1)
			return &connectivityIPOutput{
				Status:     http.StatusTooManyRequests,
				RetryAfter: strconv.Itoa(retry),
				Body:       connectivityJSON(connectivityRateLimitResponse{RetryAfterSeconds: retry}),
			}, nil
		}
		return &connectivityIPOutput{Status: http.StatusOK, Body: connectivityJSON(connectivityIPResult{IP: address.String()})}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:      "connectivity-check",
		Method:           http.MethodPost,
		Path:             "/v1/check",
		Summary:          "Probe a Heya server from the public internet",
		Description:      "TLS-dials the caller's own public source IP and requested port, then verifies a short-lived challenge at /api/connectivity/probe. The request can never select another target address.",
		Tags:             []string{"Connectivity"},
		DefaultStatus:    http.StatusOK,
		SkipValidateBody: true,
		Responses: map[string]*huma.Response{
			"200": jsonResponse("Completed outside-in connectivity check", "#/components/schemas/ConnectivityCheckResult"),
			"429": withRetryAfter(jsonResponse("Source IP rate limit or in-flight limit exceeded", "#/components/schemas/ConnectivityRateLimitResponse")),
		},
	}, func(ctx context.Context, input *connectivityCheckInput) (*connectivityCheckOutput, error) {
		if input.Body.Port < 1024 || input.Body.Port > 65535 {
			return nil, huma.Error400BadRequest("port must be between 1024 and 65535")
		}
		if !connectivityChallengePattern.MatchString(input.Body.Challenge) {
			return nil, huma.Error400BadRequest("challenge must contain 16 to 64 lowercase hexadecimal characters")
		}
		address, invalid := resolveConnectivityAddress(ctx, resolver)
		if invalid != nil {
			return nil, invalid
		}

		allowed, retry, err := limiter.Allow(ctx, "check", address.String(), 10, connectivityRateWindow)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("connectivity rate limiter is unavailable", err)
		}
		if !allowed {
			return connectivityRateLimitedOutput(retry), nil
		}
		release, acquired, retry, err := limiter.Acquire(ctx, address.String(), connectivityLockTTL)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("connectivity concurrency limiter is unavailable", err)
		}
		if !acquired {
			return connectivityRateLimitedOutput(retry), nil
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			release(releaseCtx)
		}()

		result := prober.Probe(ctx, address, input.Body.Port, input.Body.Challenge)
		outcome := "ok"
		if result.Error != nil {
			outcome = result.Error.Code
		}
		slog.InfoContext(ctx, "connectivity check",
			"ip", address.String(),
			"port", input.Body.Port,
			"outcome", outcome,
			"latency_ms", result.LatencyMS,
		)
		return &connectivityCheckOutput{Status: http.StatusOK, Body: connectivityJSON(result)}, nil
	})
}

func registerConnectivitySchemas(api huma.API) {
	registry := api.OpenAPI().Components.Schemas
	registry.Schema(reflect.TypeFor[connectivityCheckResult](), true, "ConnectivityCheckResult")
	registry.Schema(reflect.TypeFor[connectivityIPResult](), true, "ConnectivityIPResult")
	registry.Schema(reflect.TypeFor[connectivityRateLimitResponse](), true, "ConnectivityRateLimitResponse")
}

func resolveConnectivityAddress(ctx context.Context, resolver *connectivity.ClientIPResolver) (address netip.Addr, statusError huma.StatusError) {
	request, ok := requestDetailsFromContext(ctx)
	if !ok {
		return address, huma.Error400BadRequest("request source address is unavailable")
	}
	address, err := resolver.Resolve(request)
	if err != nil {
		return address, huma.Error400BadRequest(err.Error())
	}
	return address, nil
}

func connectivityRateLimitedOutput(retry int) *connectivityCheckOutput {
	retry = max(retry, 1)
	return &connectivityCheckOutput{
		Status:     http.StatusTooManyRequests,
		RetryAfter: strconv.Itoa(retry),
		Body:       connectivityJSON(connectivityRateLimitResponse{RetryAfterSeconds: retry}),
	}
}

func connectivityJSON(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal connectivity response: %v", err))
	}
	return body
}

func runtimeRedis(runtime *platform.Runtime) *redis.Client {
	if runtime == nil {
		return nil
	}
	return runtime.Redis
}
