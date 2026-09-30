package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"cortexai/internal/platform/httpx"
)

const (
	keySecret     = "test_secret"
	webhookSecret = "whsec"
)

type memStore struct {
	mu       sync.Mutex
	payments map[string]Payment
}

func (m *memStore) Create(_ context.Context, p Payment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.payments[p.OrderID] = p
	return nil
}

func (m *memStore) GetByOrder(_ context.Context, orderID string) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.payments[orderID]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	return p, nil
}

func (m *memStore) MarkPaid(_ context.Context, orderID, paymentID string) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.payments[orderID]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	if p.Status != StatusPaid {
		p.Status, p.PaymentID = StatusPaid, paymentID
		m.payments[orderID] = p
	}
	return p, nil
}

func (m *memStore) MarkFailed(context.Context, string) error { return nil }
func (m *memStore) ListByUser(context.Context, string, int) ([]Payment, error) {
	return nil, nil
}

// recordingCredits mimics the auth service: grants are idempotent per key.
type recordingCredits struct {
	mu      sync.Mutex
	granted map[string]int64
	calls   int
}

func (c *recordingCredits) Grant(_ context.Context, key, userID string, amount int64, _ string, _ int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if _, done := c.granted[key]; !done {
		c.granted[key] = amount
	}
	return nil
}

type fakeGateway struct{}

func (fakeGateway) CreateOrder(_ context.Context, amount int64, currency, _ string, _ map[string]string) (Order, error) {
	return Order{ID: "order_new", Amount: amount, Currency: currency}, nil
}

func setup() (*Handler, *memStore, *recordingCredits, http.Handler) {
	store := &memStore{payments: map[string]Payment{
		"order_1": {OrderID: "order_1", UserID: "alice", Plan: "starter", Amount: 19900, Credits: 500, Status: StatusCreated},
	}}
	credits := &recordingCredits{granted: map[string]int64{}}
	h := &Handler{
		Store: store, Gateway: fakeGateway{}, Credits: credits,
		KeyID: "rzp_test_abc", KeySecret: keySecret, WebhookSecret: webhookSecret,
	}
	r := chi.NewRouter()
	h.Routes(r)
	return h, store, credits, r
}

func request(t *testing.T, h http.Handler, path, userID string, body any, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case []byte:
		raw = b
	default:
		var err error
		raw, err = json.Marshal(b)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if userID != "" {
		req.Header.Set(httpx.HeaderUserID, userID)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func verifyBody(orderID, paymentID string) map[string]string {
	return map[string]string{
		"razorpay_order_id":   orderID,
		"razorpay_payment_id": paymentID,
		"razorpay_signature":  sign(keySecret, []byte(orderID+"|"+paymentID)),
	}
}

func TestVerifyGrantsOnce(t *testing.T) {
	_, _, credits, h := setup()
	for range 3 { // replayed verify calls
		rec := request(t, h, "/verify", "alice", verifyBody("order_1", "pay_1"), nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	require.Equal(t, map[string]int64{"order_1": 500}, credits.granted)
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	_, _, credits, h := setup()
	body := verifyBody("order_1", "pay_1")
	body["razorpay_signature"] = "deadbeef"
	rec := request(t, h, "/verify", "alice", body, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, credits.granted)
}

func TestVerifyRejectsSomeoneElsesOrder(t *testing.T) {
	_, _, credits, h := setup()
	// Mallory holds a genuinely signed payment for Alice's order.
	rec := request(t, h, "/verify", "mallory", verifyBody("order_1", "pay_1"), nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, credits.granted)
}

func webhookBody(orderID string, amount int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"event": "payment.captured",
		"payload": map[string]any{"payment": map[string]any{"entity": map[string]any{
			"id": "pay_w", "order_id": orderID, "amount": amount,
		}}},
	})
	return b
}

func TestWebhookSignatureRequired(t *testing.T) {
	_, _, credits, h := setup()
	body := webhookBody("order_1", 19900)
	rec := request(t, h, "/webhook", "", body, map[string]string{"X-Razorpay-Signature": "nope"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, credits.granted)
}

func TestWebhookAndVerifyTogetherGrantOnce(t *testing.T) {
	_, store, credits, h := setup()
	body := webhookBody("order_1", 19900)
	sig := map[string]string{"X-Razorpay-Signature": sign(webhookSecret, body)}

	require.Equal(t, http.StatusOK, request(t, h, "/webhook", "", body, sig).Code)
	require.Equal(t, http.StatusOK, request(t, h, "/webhook", "", body, sig).Code)
	require.Equal(t, http.StatusOK, request(t, h, "/verify", "alice", verifyBody("order_1", "pay_w"), nil).Code)

	require.Equal(t, map[string]int64{"order_1": 500}, credits.granted)
	p, _ := store.GetByOrder(context.Background(), "order_1")
	require.Equal(t, StatusPaid, p.Status)
}

func TestWebhookIgnoresAmountMismatch(t *testing.T) {
	_, _, credits, h := setup()
	body := webhookBody("order_1", 100)
	rec := request(t, h, "/webhook", "", body, map[string]string{"X-Razorpay-Signature": sign(webhookSecret, body)})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, credits.granted)
}

func TestCreateOrderUsesServerPrice(t *testing.T) {
	_, store, _, h := setup()
	rec := request(t, h, "/orders", "alice", map[string]string{"plan": "pro"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	p, err := store.GetByOrder(context.Background(), "order_new")
	require.NoError(t, err)
	require.EqualValues(t, 49900, p.Amount)
	require.Equal(t, "alice", p.UserID)

	rec = request(t, h, "/orders", "alice", map[string]string{"plan": "free-money"}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
