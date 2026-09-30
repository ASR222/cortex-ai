// Package internalauth protects service-to-service traffic.
//
// Only the gateway is reachable from the internet. Every other service sits on
// a private Docker network, and additionally requires a shared secret in the
// X-Internal-Token header. The second layer matters because services trust the
// X-User-Id header: if a service were ever exposed by mistake, an attacker
// still could not impersonate a user without the token.
package internalauth

import (
	"crypto/subtle"
	"net/http"

	"cortexai/internal/platform/httpx"
)

// Require rejects requests that do not carry the expected token.
// /healthz stays open so Docker health checks work without the secret.
func Require(token string) func(http.Handler) http.Handler {
	expected := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			got := []byte(r.Header.Get(httpx.HeaderInternalToken))
			// ConstantTimeCompare avoids leaking the token through timing.
			if len(expected) == 0 || subtle.ConstantTimeCompare(got, expected) != 1 {
				httpx.WriteJSON(w, http.StatusUnauthorized, &httpx.Error{
					Code: "unauthorized", Message: "Invalid internal token.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserID returns the authenticated user id forwarded by the gateway, or a 401.
// It is only trustworthy behind Require.
func UserID(r *http.Request) (string, error) {
	id := r.Header.Get(httpx.HeaderUserID)
	if id == "" {
		return "", httpx.ErrUnauthorized
	}
	return id, nil
}
