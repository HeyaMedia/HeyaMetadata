package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type HTTPClient struct {
	client   *http.Client
	resolver PayloadResolver
}

func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{client: &http.Client{Timeout: timeout}}
}

func NewCachedHTTPClient(timeout time.Duration, resolver PayloadResolver) *HTTPClient {
	return &HTTPClient{client: &http.Client{Timeout: timeout}, resolver: resolver}
}

func (c *HTTPClient) Do(ctx context.Context, request *http.Request, payload Payload) (Payload, error) {
	return c.DoGuarded(ctx, request, payload, nil)
}

// DoGuarded runs guard only when an actual network request is necessary. This
// lets a warm shared response be reused without requiring provider credentials.
func (c *HTTPClient) DoGuarded(ctx context.Context, request *http.Request, payload Payload, guard func() error) (Payload, error) {
	return c.DoClassified(ctx, request, payload, guard, nil)
}

// DoClassified applies classify after a network response but before shared
// persistence/cache policy. Cached responses were classified when fetched.
func (c *HTTPClient) DoClassified(ctx context.Context, request *http.Request, payload Payload, guard func() error, classify func(*Payload)) (Payload, error) {
	fetch := func() (Payload, error) {
		if guard != nil {
			if err := guard(); err != nil {
				return Payload{}, err
			}
		}
		result, err := c.doNetwork(ctx, request, payload)
		if err == nil && classify != nil {
			classify(&result)
		}
		return result, err
	}
	if c.resolver != nil {
		return c.resolver.Resolve(ctx, payload, fetch)
	}
	return fetch()
}

func (c *HTTPClient) doNetwork(ctx context.Context, request *http.Request, payload Payload) (Payload, error) {
	request = request.WithContext(ctx)
	startedAt := time.Now()
	response, err := c.client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return Payload{}, fmt.Errorf("send %s provider request: %w", payload.Provider, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return Payload{}, fmt.Errorf("read %s provider response: %w", payload.Provider, err)
	}
	payload.StatusCode = response.StatusCode
	payload.Headers = response.Header.Clone()
	payload.Body = body
	payload.ObservedAt = time.Now().UTC()
	payload.ResponseTime = time.Since(startedAt)
	return payload, nil
}
