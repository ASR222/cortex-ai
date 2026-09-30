package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
)

// Handler serves the billing API.
type Handler struct {
	Store         Store
	Gateway       Gateway
	Credits       Credits
	KeyID         string // public Razorpay key id, sent to the browser
	KeySecret     string
	WebhookSecret string // empty disables the webhook endpoint
}

// Routes registers the handler's endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/plans", httpx.Handle(h.plans))
	r.Get("/payments", httpx.Handle(h.payments))
	r.Post("/orders", httpx.Handle(h.createOrder))
	r.Post("/verify", httpx.Handle(h.verify))
	r.Post("/webhook", httpx.Handle(h.webhook))
}

func (h *Handler) plans(w http.ResponseWriter, _ *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"plans":    PlanList(),
		"testMode": len(h.KeyID) > 9 && h.KeyID[:9] == "rzp_test_",
	})
	return nil
}

func (h *Handler) payments(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	list, err := h.Store.ListByUser(r.Context(), userID, 50)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	var in struct {
		Plan string `json:"plan"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 1<<10); err != nil {
		return err
	}
	plan, ok := Plans[in.Plan]
	if !ok {
		return httpx.BadRequest("Unknown plan.")
	}

	amount := plan.AmountINR * 100
	receipt := fmt.Sprintf("cx_%d", time.Now().UnixNano())
	order, err := h.Gateway.CreateOrder(r.Context(), amount, "INR", receipt, map[string]string{
		"userId": userID,
		"plan":   plan.ID,
	})
	if err != nil {
		return err
	}
	err = h.Store.Create(r.Context(), Payment{
		UserID:   userID,
		OrderID:  order.ID,
		Plan:     plan.ID,
		Amount:   order.Amount,
		Currency: order.Currency,
		Credits:  plan.Credits,
		Status:   StatusCreated,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"order": order,
		"plan":  plan,
		"keyId": h.KeyID,
	})
	return nil
}

// fulfill marks the order paid and grants its credits.
//
// Safe to call any number of times: MarkPaid is a no-op on a paid payment and
// the credit grant is keyed by order id. Grant is attempted on every call, so
// if a previous attempt marked the order paid but crashed before granting,
// the next verify/webhook retry completes it.
func (h *Handler) fulfill(ctx context.Context, p Payment, paymentID string) error {
	paid, err := h.Store.MarkPaid(ctx, p.OrderID, paymentID)
	if err != nil {
		return err
	}
	plan, ok := Plans[paid.Plan]
	if !ok {
		return fmt.Errorf("payment %s has unknown plan %q", paid.OrderID, paid.Plan)
	}
	return h.Credits.Grant(ctx, paid.OrderID, paid.UserID, paid.Credits, plan.ID, plan.ValidityDays)
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	var in struct {
		OrderID   string `json:"razorpay_order_id"`
		PaymentID string `json:"razorpay_payment_id"`
		Signature string `json:"razorpay_signature"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 2<<10); err != nil {
		return err
	}
	if in.OrderID == "" || in.PaymentID == "" || in.Signature == "" {
		return httpx.BadRequest("Missing payment fields.")
	}
	if !VerifyCheckoutSignature(h.KeySecret, in.OrderID, in.PaymentID, in.Signature) {
		return httpx.NewError(http.StatusBadRequest, "invalid_signature", "Payment verification failed.")
	}

	p, err := h.Store.GetByOrder(r.Context(), in.OrderID)
	// A valid signature proves Razorpay saw a payment for this order, not
	// that the order belongs to the caller. Without this check a user could
	// replay someone else's signed payment to credit their own account.
	if errors.Is(err, ErrPaymentNotFound) || (err == nil && p.UserID != userID) {
		return httpx.NewError(http.StatusNotFound, "not_found", "Payment not found.")
	}
	if err != nil {
		return err
	}
	if err := h.fulfill(r.Context(), p, in.PaymentID); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
	return nil
}

type webhookEvent struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID      string `json:"id"`
				OrderID string `json:"order_id"`
				Amount  int64  `json:"amount"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// webhook receives Razorpay server-to-server events. It is the reliable path:
// it still credits the user if they close the tab before /verify runs.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) error {
	if h.WebhookSecret == "" {
		return httpx.ErrNotFound
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		return httpx.BadRequest("Could not read body.")
	}
	if !VerifyWebhookSignature(h.WebhookSecret, body, r.Header.Get("X-Razorpay-Signature")) {
		return httpx.NewError(http.StatusBadRequest, "invalid_signature", "Invalid webhook signature.")
	}

	var ev webhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return httpx.BadRequest("Invalid JSON.")
	}
	entity := ev.Payload.Payment.Entity
	ctx := r.Context()

	switch ev.Event {
	case "payment.captured", "order.paid":
		p, err := h.Store.GetByOrder(ctx, entity.OrderID)
		if errors.Is(err, ErrPaymentNotFound) {
			// Not ours (e.g. another app on the same account). Acknowledge
			// so Razorpay stops retrying.
			slog.WarnContext(ctx, "webhook for unknown order", "order", entity.OrderID)
			break
		}
		if err != nil {
			return err
		}
		if entity.Amount != p.Amount {
			slog.ErrorContext(ctx, "webhook amount mismatch", "order", p.OrderID, "got", entity.Amount, "want", p.Amount)
			break
		}
		if err := h.fulfill(ctx, p, entity.ID); err != nil {
			return err // non-2xx makes Razorpay retry later
		}
	case "payment.failed":
		if err := h.Store.MarkFailed(ctx, entity.OrderID); err != nil {
			return err
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}
