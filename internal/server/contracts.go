package server

import "github.com/danielgtaylor/huma/v2"

func jsonResponse(description, schemaRef string) *huma.Response {
	return &huma.Response{
		Description: description,
		Content: map[string]*huma.MediaType{
			"application/json": {Schema: &huma.Schema{Ref: schemaRef}},
		},
	}
}

func acceptedJSONResponse(schemaRef string) *huma.Response {
	response := jsonResponse("Accepted for asynchronous processing", schemaRef)
	return withRetryAfter(response)
}

func retryableJSONResponse(schemaRef string) *huma.Response {
	response := jsonResponse("Processing failed after retries; the request may be submitted again", schemaRef)
	return withRetryAfter(response)
}

func withRetryAfter(response *huma.Response) *huma.Response {
	response.Headers = map[string]*huma.Param{
		"Retry-After": {
			Description: "Suggested number of seconds before polling the returned resource or job",
			Schema:      &huma.Schema{Type: "string"},
		},
	}
	return response
}

func binaryResponse(description string, mediaTypes ...string) *huma.Response {
	content := make(map[string]*huma.MediaType, len(mediaTypes))
	for _, mediaType := range mediaTypes {
		content[mediaType] = &huma.MediaType{Schema: &huma.Schema{Type: "string", Format: "binary"}}
	}
	return &huma.Response{Description: description, Content: content}
}
