package sandbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SandboxError is the base error type for all SDK errors.
type SandboxError struct {
	Message string
}

func (e *SandboxError) Error() string { return e.Message }

// NotFoundError is returned when a sandbox or resource is not found (HTTP 404).
type NotFoundError struct{ SandboxError }

// AuthError is returned on authentication or authorisation failure (HTTP 401/403).
type AuthError struct{ SandboxError }

// TimeoutError is returned when a request or operation times out (HTTP 408/504).
type TimeoutError struct{ SandboxError }

// NotEnoughSpaceError is returned when the sandbox has no disk space (HTTP 507).
type NotEnoughSpaceError struct{ SandboxError }

// InvalidArgumentError is returned when the caller supplies bad input (HTTP 400/422).
type InvalidArgumentError struct{ SandboxError }

// RateLimitError is returned when the API rate limit is exceeded (HTTP 429).
type RateLimitError struct{ SandboxError }

// CommandExitError is returned by CommandHandle.Wait when a process exits with a non-zero code.
type CommandExitError struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func (e *CommandExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.ExitCode)
}

// apiErrorBody is used to decode JSON error responses from the gateway.
type apiErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// parseAPIError converts an HTTP status code and response body into a typed SDK error.
// It tries to decode a JSON body of the form {"code": N, "message": "..."}.
// If parsing fails the raw body is used as the message.
func parseAPIError(statusCode int, body []byte) error {
	var apiErr apiErrorBody
	msg := ""
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		msg = apiErr.Message
	} else {
		msg = string(body)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", statusCode)
		}
	}

	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AuthError{SandboxError{Message: msg}}
	case http.StatusNotFound:
		return &NotFoundError{SandboxError{Message: msg}}
	case http.StatusTooManyRequests:
		return &RateLimitError{SandboxError{Message: msg}}
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return &TimeoutError{SandboxError{Message: msg}}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &InvalidArgumentError{SandboxError{Message: msg}}
	case http.StatusInsufficientStorage:
		return &NotEnoughSpaceError{SandboxError{Message: msg}}
	default:
		return &SandboxError{Message: msg}
	}
}

// msToTimeout converts a millisecond count to time.Duration.
func msToTimeout(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
