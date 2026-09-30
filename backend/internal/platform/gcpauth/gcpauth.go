// Package gcpauth fetches Google-signed identity tokens for service-to-service
// calls on Cloud Run.
//
// On Cloud Run, internal services are deployed with --no-allow-unauthenticated.
// Google's front end then rejects any request that does not carry an identity
// token for a service account that holds roles/run.invoker on the target. The
// token comes from the metadata server, which is only reachable from inside
// Google Cloud and returns a token for the service's own identity. No key
// files exist anywhere.
package gcpauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// TokenSource returns a bearer token for calling audience.
type TokenSource interface {
	Token(ctx context.Context, audience string) (string, error)
}

const identityURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"

// MetadataIDTokens fetches and caches identity tokens from the GCE/Cloud Run
// metadata server, one per audience.
type MetadataIDTokens struct {
	URL  string
	HTTP *http.Client

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	token   string
	refresh time.Time
}

// FromEnv returns a token source when SERVICE_AUTH=google, or nil otherwise
// (local development and Docker Compose).
func FromEnv() TokenSource {
	if strings.ToLower(os.Getenv("SERVICE_AUTH")) != "google" {
		return nil
	}
	return &MetadataIDTokens{URL: identityURL, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// Token returns a cached identity token for audience, refreshing it five
// minutes before it expires.
func (m *MetadataIDTokens) Token(ctx context.Context, audience string) (string, error) {
	m.mu.Lock()
	if c, ok := m.cache[audience]; ok && time.Now().Before(c.refresh) {
		m.mu.Unlock()
		return c.token, nil
	}
	m.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL+"?audience="+url.QueryEscape(audience), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata identity token: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata identity token: status %d", resp.StatusCode)
	}
	token := strings.TrimSpace(string(body))

	refresh := time.Now().Add(55 * time.Minute)
	if exp, ok := expiry(token); ok {
		refresh = exp.Add(-5 * time.Minute)
	}
	m.mu.Lock()
	if m.cache == nil {
		m.cache = map[string]cached{}
	}
	m.cache[audience] = cached{token: token, refresh: refresh}
	m.mu.Unlock()
	return token, nil
}

// expiry reads the exp claim without verifying the token (we only need to
// know when to refresh our own token; the receiver verifies it).
func expiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}
