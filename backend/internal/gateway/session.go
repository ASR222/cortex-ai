// Package gateway is the only internet-facing service. It authenticates the
// session cookie, applies rate limits and CSRF checks, and forwards requests
// to the internal services with a trusted X-User-Id header.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNoSession means the session id is unknown or expired.
var ErrNoSession = errors.New("no session")

// Sessions stores login sessions in Redis.
//
// The cookie holds a random 256-bit token. Redis stores only its SHA-256
// hash, so someone who can read Redis still cannot hijack a session.
// Sessions are server-side (not JWTs) so logout and revocation take effect
// immediately.
type Sessions struct {
	Redis *redis.Client
	TTL   time.Duration
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "session:" + hex.EncodeToString(sum[:])
}

// Create starts a session for userID and returns the cookie token.
func (s *Sessions) Create(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	if err := s.Redis.Set(ctx, sessionKey(token), userID, s.TTL).Err(); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// Get returns the user id for token.
func (s *Sessions) Get(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 128 {
		return "", ErrNoSession
	}
	userID, err := s.Redis.Get(ctx, sessionKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", fmt.Errorf("get session: %w", err)
	}
	return userID, nil
}

// Delete ends a session. Deleting an unknown session is not an error.
func (s *Sessions) Delete(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.Redis.Del(ctx, sessionKey(token)).Err()
}
