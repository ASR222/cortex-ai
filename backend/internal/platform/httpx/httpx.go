// Package httpx holds the small HTTP helpers shared by every Go service:
// JSON encoding, a typed API error, and common middleware.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Error is an error that maps directly to an HTTP response.
//
// Code is a stable machine-readable identifier the frontend can switch on
// (for example "insufficient_credits"); Message is safe to show to users.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

// NewError builds an *Error.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Common errors.
var (
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "Please sign in to continue.")
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "Resource not found.")
)

// BadRequest is shorthand for a 400 validation error.
func BadRequest(message string) *Error {
	return NewError(http.StatusBadRequest, "bad_request", message)
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError writes err as a JSON error body. Unknown errors become a generic
// 500 so internal details never leak to clients; they are logged instead.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		WriteJSON(w, apiErr.Status, apiErr)
		return
	}
	slog.ErrorContext(r.Context(), "unhandled error", "err", err, "path", r.URL.Path)
	WriteJSON(w, http.StatusInternalServerError, &Error{
		Code:    "internal",
		Message: "Something went wrong. Please try again.",
	})
}

// DecodeJSON reads a JSON body into dst, rejecting unknown fields and bodies
// larger than maxBytes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return NewError(http.StatusRequestEntityTooLarge, "too_large", "Request body is too large.")
		}
		return BadRequest("Invalid JSON body.")
	}
	if dec.More() {
		return BadRequest("Request body must contain a single JSON object.")
	}
	return nil
}

// HandlerFunc is an http handler that returns an error instead of writing it.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts a HandlerFunc to http.HandlerFunc, writing any returned error.
func Handle(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// DrainAndClose discards the rest of a response body so the connection can be
// reused.
func DrainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	_ = body.Close()
}
