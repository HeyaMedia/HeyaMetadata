package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{client: &http.Client{Timeout: timeout}}
}

func (c *HTTPClient) Do(ctx context.Context, request *http.Request, payload Payload) (Payload, error) {
	request = request.WithContext(ctx)
	startedAt := time.Now()
	response, err := c.client.Do(request)
	if err != nil {
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
