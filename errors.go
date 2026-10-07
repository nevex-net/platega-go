package platega

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Categories of API failures. Test for them with errors.Is:
//
//	if errors.Is(err, platega.ErrRateLimited) { ... }
var (
	ErrBadRequest   = errors.New("platega: bad request")  // HTTP 400
	ErrUnauthorized = errors.New("platega: unauthorized") // HTTP 401
	ErrNotFound     = errors.New("platega: not found")    // HTTP 404
	ErrRateLimited  = errors.New("platega: rate limited") // HTTP 429
	ErrServer       = errors.New("platega: server error") // HTTP 5xx
)

// APIError is returned when the API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	// Message is the "message" field of a JSON error body, if there is one.
	Message string
	// Body is the raw response body.
	Body []byte
	// Header holds the response headers, for example a request ID to quote to
	// Platega support.
	Header http.Header
	// RetryAfter is the server's Retry-After hint, or zero.
	RetryAfter time.Duration
}

func newAPIError(r request, resp response) *APIError {
	e := &APIError{
		StatusCode: resp.status,
		Method:     r.method,
		Path:       r.path,
		Body:       resp.body,
		Header:     resp.header,
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(resp.body, &body) == nil {
		e.Message = strings.TrimSpace(body.Message)
	}
	if d, ok := parseRetryAfter(resp.header.Get("Retry-After"), time.Now()); ok {
		e.RetryAfter = d
	}
	return e
}

func (e *APIError) Error() string {
	detail := e.Message
	if detail == "" {
		const limit = 200
		detail = string(e.Body)
		if utf8.RuneCountInString(detail) > limit {
			detail = string([]rune(detail)[:limit]) + "…"
		}
	}
	if detail == "" {
		return fmt.Sprintf("platega: %s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("platega: %s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, detail)
}

// Is lets errors.Is match an APIError against the Err* categories.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= 500
	}
	return false
}

// IsNotFound reports whether err is an API 404 response.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// IsUnauthorized reports whether err is an API 401 response, which usually
// means a wrong merchant ID or secret.
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

// IsBadRequest reports whether err is an API 400 response.
func IsBadRequest(err error) bool { return errors.Is(err, ErrBadRequest) }
