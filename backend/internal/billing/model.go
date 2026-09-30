// Package billing sells credit packs through Razorpay.
//
// Flow:
//  1. The user picks a plan; we create a Razorpay order and a local Payment
//     record tied to that user (status "created").
//  2. Razorpay Checkout runs in the browser and returns a signed payment.
//  3. Credits are granted by whichever arrives first: the browser's /verify
//     call or Razorpay's server-to-server webhook. Both paths call fulfill,
//     which is idempotent, so the user is credited exactly once even if both
//     arrive, or if either is retried or replayed.
package billing

import (
	"context"
	"errors"
	"time"
)

// Plan is a purchasable credit pack.
type Plan struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	AmountINR    int64  `json:"amount"`
	Credits      int64  `json:"credits"`
	ValidityDays int    `json:"validityDays"`
}

// Plans are the packs for sale. Prices live on the server so a client cannot
// choose its own price.
var Plans = map[string]Plan{
	"starter": {ID: "starter", Name: "Starter", AmountINR: 199, Credits: 500, ValidityDays: 30},
	"pro":     {ID: "pro", Name: "Pro", AmountINR: 499, Credits: 1000, ValidityDays: 30},
}

// PlanList returns Plans in display order.
func PlanList() []Plan { return []Plan{Plans["starter"], Plans["pro"]} }

// Payment statuses.
const (
	StatusCreated = "created"
	StatusPaid    = "paid"
	StatusFailed  = "failed"
)

// Payment tracks one Razorpay order.
type Payment struct {
	ID        string    `json:"_id" bson:"_id"`
	UserID    string    `json:"-" bson:"userId"`
	OrderID   string    `json:"orderId" bson:"orderId"`
	PaymentID string    `json:"paymentId,omitempty" bson:"paymentId,omitempty"`
	Plan      string    `json:"plan" bson:"plan"`
	Amount    int64     `json:"amount" bson:"amount"` // in paise
	Currency  string    `json:"currency" bson:"currency"`
	Credits   int64     `json:"credits" bson:"credits"`
	Status    string    `json:"status" bson:"status"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updatedAt"`
}

// ErrPaymentNotFound means no payment exists for the order.
var ErrPaymentNotFound = errors.New("payment not found")

// Store persists payments.
type Store interface {
	Create(ctx context.Context, p Payment) error
	GetByOrder(ctx context.Context, orderID string) (Payment, error)
	// MarkPaid moves a payment to "paid" (from created or failed) and
	// returns it. Calling it again on a paid payment is a no-op.
	MarkPaid(ctx context.Context, orderID, paymentID string) (Payment, error)
	MarkFailed(ctx context.Context, orderID string) error
	ListByUser(ctx context.Context, userID string, limit int) ([]Payment, error)
}

// Order is the subset of a Razorpay order we use.
type Order struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Gateway creates orders with the payment provider.
type Gateway interface {
	CreateOrder(ctx context.Context, amountPaise int64, currency, receipt string, notes map[string]string) (Order, error)
}

// Credits grants purchased credits (implemented by the auth service client).
type Credits interface {
	Grant(ctx context.Context, key, userID string, amount int64, plan string, validityDays int) error
}
