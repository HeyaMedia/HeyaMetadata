package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

const problemBaseURL = "https://heya.media/problems/"

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
		return &huma.ErrorModel{Type: problemType, Title: http.StatusText(status), Status: status, Detail: message, Errors: details}
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
