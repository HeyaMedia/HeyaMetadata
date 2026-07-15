package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

const problemBaseURL = "https://heya.media/problems/"

// ErrorModel is the service-wide RFC 9457 problem representation. Code and
// change-stream metadata are optional extensions used when a client can
// recover programmatically from a synchronization conflict.
type ErrorModel struct {
	Type       string              `json:"type,omitempty" format:"uri" default:"about:blank" example:"https://example.com/errors/example" doc:"A URI reference to human-readable documentation for the error."`
	Title      string              `json:"title,omitempty" example:"Bad Request" doc:"A short, human-readable summary of the problem type. This value should not change between occurrences of the error."`
	Status     int                 `json:"status,omitempty" example:"400" doc:"HTTP status code"`
	Detail     string              `json:"detail,omitempty" example:"Property foo is required but is missing." doc:"A human-readable explanation specific to this occurrence of the problem."`
	Instance   string              `json:"instance,omitempty" format:"uri" example:"https://example.com/error-log/abc123" doc:"A URI reference that identifies the specific occurrence of the problem."`
	Errors     []*huma.ErrorDetail `json:"errors,omitempty" doc:"Optional list of individual error details"`
	Code       string              `json:"code,omitempty" doc:"Stable machine-readable problem code"`
	StreamID   string              `json:"stream_id,omitempty" format:"uuid" doc:"Current persistent change-stream identity"`
	HeadCursor *int64              `json:"head_cursor,omitempty" minimum:"0" doc:"Highest currently available public change cursor"`
}

func (problem *ErrorModel) Error() string  { return problem.Detail }
func (problem *ErrorModel) GetStatus() int { return problem.Status }
func (problem *ErrorModel) ContentType(string) string {
	return "application/problem+json"
}

func init() {
	huma.NewError = func(status int, message string, errs ...error) huma.StatusError {
		details := make([]*huma.ErrorDetail, 0, len(errs))
		for _, err := range errs {
			if err == nil {
				continue
			}
			if converted, ok := err.(huma.ErrorDetailer); ok {
				details = append(details, converted.ErrorDetail())
			} else {
				details = append(details, &huma.ErrorDetail{Message: err.Error()})
			}
		}
		problemType := "about:blank"
		if status > 0 {
			problemType = problemBaseURL + problemTypeName(status)
		}
		return &ErrorModel{Type: problemType, Title: http.StatusText(status), Status: status, Detail: message, Errors: details}
	}
}

func changeFeedConflict(code, detail, streamID string, headCursor int64) *ErrorModel {
	return &ErrorModel{
		Type:       problemBaseURL + code,
		Title:      http.StatusText(http.StatusConflict),
		Status:     http.StatusConflict,
		Detail:     detail,
		Code:       code,
		StreamID:   streamID,
		HeadCursor: &headCursor,
	}
}

func problemTypeName(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid-request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not-found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "validation-failed"
	case http.StatusTooManyRequests:
		return "rate-limited"
	case http.StatusBadGateway:
		return "upstream-failure"
	case http.StatusServiceUnavailable:
		return "service-unavailable"
	case http.StatusGatewayTimeout:
		return "upstream-timeout"
	case http.StatusInternalServerError:
		return "internal-error"
	default:
		return "http-error"
	}
}
