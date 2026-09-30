package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

// Header names shared between services.
const (
	HeaderRequestID     = "X-Request-Id"
	HeaderUserID        = "X-User-Id"
	HeaderInternalToken = "X-Internal-Token"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestID returns the request id stored in ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithRequestID stores id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDMiddleware reuses an incoming X-Request-Id (set by the gateway) or
// creates one, so a single user request can be traced across every service.
//
// trustIncoming must be false on the public edge: a client-supplied id is
// replaced so users cannot forge log correlation ids.
func RequestIDMiddleware(trustIncoming bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if trustIncoming {
				id = r.Header.Get(HeaderRequestID)
			}
			if id == "" || len(id) > 64 {
				id = uuid.NewString()
			}
			r.Header.Set(HeaderRequestID, id)
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush keeps streaming (SSE) working through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger logs one structured line per request.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		slog.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestID(r.Context()),
		)
	})
}

// Recoverer turns panics into 500 responses instead of killing the process.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "panic", "value", rec, "stack", string(debug.Stack()))
				WriteJSON(w, http.StatusInternalServerError, &Error{Code: "internal", Message: "Something went wrong."})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Healthz is a liveness endpoint.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
