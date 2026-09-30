package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type stubVerifier struct{ err error }

func (s stubVerifier) Verify(context.Context, string) (Identity, error) {
	return Identity{UID: "uid"}, s.err
}

// stubStore implements only what the handler tests exercise.
type stubStore struct {
	Store
	user       User
	reserveErr error
}

func (s *stubStore) UpsertUser(context.Context, Identity, int64) (User, error) { return s.user, nil }
func (s *stubStore) GetUser(_ context.Context, id string) (User, error) {
	if id != s.user.ID {
		return User{}, ErrUserNotFound
	}
	return s.user, nil
}
func (s *stubStore) Reserve(context.Context, string, int64, string) (Reservation, int64, error) {
	return Reservation{ID: "r1"}, 9, s.reserveErr
}

func do(t *testing.T, h *Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.Routes(r)
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func TestLoginRejectsInvalidToken(t *testing.T) {
	h := &Handler{Store: &stubStore{}, Verifier: stubVerifier{err: ErrInvalidToken}, Now: time.Now}
	rec := do(t, h, http.MethodPost, "/internal/login", map[string]string{"idToken": "x"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestReserveInsufficientReturns402(t *testing.T) {
	h := &Handler{Store: &stubStore{reserveErr: ErrInsufficientCredits}, Now: time.Now}
	rec := do(t, h, http.MethodPost, "/internal/credits/reserve", map[string]any{"userId": "u", "amount": 5})
	require.Equal(t, http.StatusPaymentRequired, rec.Code)
	require.Contains(t, rec.Body.String(), "insufficient_credits")
}

func TestReserveValidatesAmount(t *testing.T) {
	h := &Handler{Store: &stubStore{}, Now: time.Now}
	for _, amount := range []int{0, -5, 5000} {
		rec := do(t, h, http.MethodPost, "/internal/credits/reserve", map[string]any{"userId": "u", "amount": amount})
		require.Equal(t, http.StatusBadRequest, rec.Code, "amount %d", amount)
	}
}

func TestGetUserShowsExpiredPlanAsFree(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	h := &Handler{Store: &stubStore{user: User{ID: "u1", Plan: "pro", PlanExpiresAt: &past}}, Now: time.Now}
	rec := do(t, h, http.MethodGet, "/internal/users/u1", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"plan":"free"`)
}
