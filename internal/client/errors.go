package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrorDetail is a request-schema validation detail (Huma).
type ErrorDetail struct {
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

// APIError is an RFC 9457 problem returned by Pulse.
type APIError struct {
	StatusCode int           `json:"status"`
	Type       string        `json:"type,omitempty"`
	Title      string        `json:"title,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	Code       string        `json:"code"`
	Errors     []ErrorDetail `json:"errors,omitempty"`

	Method string `json:"-"`
	Path   string `json:"-"`
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pulse API %s %s: %d", e.Method, e.Path, e.StatusCode)
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	if e.Detail != "" {
		b.WriteString(": " + e.Detail)
	} else if e.Title != "" {
		b.WriteString(": " + e.Title)
	}
	var parts []string
	for _, d := range e.Errors {
		if d.Location != "" {
			parts = append(parts, d.Location+": "+d.Message)
		} else {
			parts = append(parts, d.Message)
		}
	}
	if len(parts) > 0 {
		b.WriteString(" (" + strings.Join(parts, "; ") + ")")
	}
	switch e.Code {
	case "unauthorized":
		b.WriteString(". Personal access tokens work on the loopback admin listener (e.g. http://127.0.0.1:8080, " +
			"through an SSH tunnel); on the public dashboard host only when the server allows it and from the token's own " +
			"networks. They must not be revoked or expired")
	case "forbidden":
		b.WriteString(". The token's role does not allow this; most resources need an administrator token")
	}
	return b.String()
}

// TransportError is a network-level failure (after retries).
type TransportError struct {
	Method, Path string
	Err          error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("pulse API %s %s: %v", e.Method, e.Path, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// decodeProblem turns an error response into an *APIError. Bodies that are not problem documents are summarised by
// status only (they are never echoed, so a misbehaving proxy cannot leak data into logs).
func decodeProblem(method, path string, resp *response) *APIError {
	e := &APIError{}
	if err := json.Unmarshal(resp.body, e); err != nil || (e.Code == "" && e.Detail == "" && e.Title == "") {
		e = &APIError{Title: http.StatusText(resp.status)}
	}
	e.StatusCode = resp.status
	e.Method, e.Path = method, path
	if e.Code == "" {
		e.Code = defaultCode(resp.status)
	}
	return e
}

func defaultCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "precondition"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusUnprocessableEntity, http.StatusBadRequest:
		return "invalid"
	}
	if status >= 500 {
		return "internal"
	}
	return ""
}

// IsNotFound reports whether err is a 404 problem.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
}

// HasStatus reports whether err is a problem with the given HTTP status.
func HasStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == status
}

// notFound is returned by lookups that scan a list and find nothing.
func notFound(path, what string) *APIError {
	return &APIError{StatusCode: http.StatusNotFound, Code: "not_found", Detail: what + " not found", Method: http.MethodGet, Path: path}
}
