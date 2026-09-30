package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cortexai/internal/platform/httpx"
)

// Razorpay is a minimal client for the Orders API. The official Go SDK is
// avoided because we need exactly one call.
type Razorpay struct {
	KeyID     string
	KeySecret string
	BaseURL   string
	HTTP      *http.Client
}

// NewRazorpay returns a client for the live Razorpay API (test or live mode
// is decided by which keys you use).
func NewRazorpay(keyID, keySecret string) *Razorpay {
	return &Razorpay{
		KeyID:     keyID,
		KeySecret: keySecret,
		BaseURL:   "https://api.razorpay.com/v1",
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateOrder creates a Razorpay order.
func (c *Razorpay) CreateOrder(ctx context.Context, amountPaise int64, currency, receipt string, notes map[string]string) (Order, error) {
	body, _ := json.Marshal(map[string]any{
		"amount":   amountPaise,
		"currency": currency,
		"receipt":  receipt,
		"notes":    notes,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return Order{}, err
	}
	req.SetBasicAuth(c.KeyID, c.KeySecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Order{}, fmt.Errorf("razorpay create order: %w", err)
	}
	defer httpx.DrainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Order{}, fmt.Errorf("razorpay create order: status %d", resp.StatusCode)
	}
	var o Order
	if err := json.NewDecoder(resp.Body).Decode(&o); err != nil {
		return Order{}, fmt.Errorf("razorpay create order: decode: %w", err)
	}
	return o, nil
}

// sign returns the hex HMAC-SHA256 of msg.
func sign(secret string, msg []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(msg)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyCheckoutSignature checks the signature Razorpay Checkout returns to
// the browser: HMAC_SHA256(order_id + "|" + payment_id, key_secret).
func VerifyCheckoutSignature(keySecret, orderID, paymentID, signature string) bool {
	expected := sign(keySecret, []byte(orderID+"|"+paymentID))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyWebhookSignature checks X-Razorpay-Signature against the raw body.
// It must be computed over the exact bytes received, before any JSON parsing.
func VerifyWebhookSignature(webhookSecret string, body []byte, signature string) bool {
	expected := sign(webhookSecret, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}
