package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/platform/httpx"
)

// Handler exposes the auth service's internal API. Every route is behind
// internalauth.Require; none of them is reachable from the internet.
type Handler struct {
	Store           Store
	Verifier        TokenVerifier
	StartingCredits int64
	Now             func() time.Time
}

// Routes registers the handler's endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/internal/login", httpx.Handle(h.login))
	r.Get("/internal/users/{id}", httpx.Handle(h.getUser))
	r.Post("/internal/credits/reserve", httpx.Handle(h.reserve))
	r.Post("/internal/credits/{id}/commit", httpx.Handle(h.commit))
	r.Post("/internal/credits/{id}/refund", httpx.Handle(h.refund))
	r.Post("/internal/credits/grant", httpx.Handle(h.grant))
}

func (h *Handler) present(u User) User {
	u.Plan = u.EffectivePlan(h.Now())
	return u
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDToken string `json:"idToken"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 16<<10); err != nil {
		return err
	}
	if in.IDToken == "" {
		return httpx.BadRequest("idToken is required.")
	}
	identity, err := h.Verifier.Verify(r.Context(), in.IDToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			return httpx.NewError(http.StatusUnauthorized, "invalid_token", "Sign-in failed. Please try again.")
		}
		return err
	}
	u, err := h.Store.UpsertUser(r.Context(), identity, h.StartingCredits)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": h.present(u)})
	return nil
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) error {
	u, err := h.Store.GetUser(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrUserNotFound) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": h.present(u)})
	return nil
}

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		UserID string `json:"userId"`
		Amount int64  `json:"amount"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 4<<10); err != nil {
		return err
	}
	if in.UserID == "" || in.Amount <= 0 || in.Amount > 1000 {
		return httpx.BadRequest("userId and a positive amount are required.")
	}
	res, balance, err := h.Store.Reserve(r.Context(), in.UserID, in.Amount, in.Reason)
	switch {
	case errors.Is(err, ErrInsufficientCredits):
		return &httpx.Error{
			Status:  http.StatusPaymentRequired,
			Code:    "insufficient_credits",
			Title:   "Out of credits",
			Message: "You don't have enough credits for this request. Upgrade your plan to continue.",
		}
	case errors.Is(err, ErrUserNotFound):
		return httpx.ErrNotFound
	case err != nil:
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"reservationId": res.ID, "credits": balance})
	return nil
}

func (h *Handler) commit(w http.ResponseWriter, r *http.Request) error {
	err := h.Store.Commit(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrReservationNotFound) {
		return httpx.ErrNotFound
	}
	if errors.Is(err, ErrReservationSettled) {
		return httpx.NewError(http.StatusConflict, "already_settled", "Reservation was already refunded.")
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (h *Handler) refund(w http.ResponseWriter, r *http.Request) error {
	balance, err := h.Store.Refund(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrReservationNotFound) {
		return httpx.ErrNotFound
	}
	if errors.Is(err, ErrReservationSettled) {
		return httpx.NewError(http.StatusConflict, "already_settled", "Reservation was already committed.")
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "credits": balance})
	return nil
}

func (h *Handler) grant(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Key          string `json:"key"`
		UserID       string `json:"userId"`
		Amount       int64  `json:"amount"`
		Plan         string `json:"plan"`
		ValidityDays int    `json:"validityDays"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 4<<10); err != nil {
		return err
	}
	if in.Key == "" || in.UserID == "" || in.Amount <= 0 || in.Plan == "" || in.ValidityDays <= 0 {
		return httpx.BadRequest("key, userId, amount, plan and validityDays are required.")
	}
	applied, err := h.Store.Grant(r.Context(), in.Key, in.UserID, in.Amount, in.Plan,
		time.Duration(in.ValidityDays)*24*time.Hour)
	if errors.Is(err, ErrUserNotFound) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"applied": applied})
	return nil
}
