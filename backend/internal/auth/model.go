// Package auth owns users and their credit balance.
//
// It never talks to the internet except to fetch Google's token-signing keys.
// The gateway calls it to log users in, and the agent and billing services
// call it to move credits.
package auth

import (
	"context"
	"errors"
	"time"
)

// User is a CortexAI account. JSON uses "_id" to match the frontend.
type User struct {
	ID            string     `json:"_id" bson:"_id"`
	FirebaseUID   string     `json:"-" bson:"firebaseUid"`
	Email         string     `json:"email" bson:"email"`
	Name          string     `json:"name" bson:"name"`
	Avatar        string     `json:"avatar" bson:"avatar"`
	Provider      string     `json:"provider" bson:"provider"`
	Plan          string     `json:"plan" bson:"plan"`
	Credits       int64      `json:"credits" bson:"credits"`
	TotalCredits  int64      `json:"totalCredits" bson:"totalCredits"`
	PlanExpiresAt *time.Time `json:"planExpiresAt,omitempty" bson:"planExpiresAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt" bson:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt" bson:"updatedAt"`
}

// EffectivePlan returns "free" once a paid plan has expired.
func (u User) EffectivePlan(now time.Time) string {
	if u.Plan != "free" && u.PlanExpiresAt != nil && now.After(*u.PlanExpiresAt) {
		return "free"
	}
	return u.Plan
}

// Reservation is a hold on credits for one agent request.
//
// Credits are taken when the request starts (so two parallel requests cannot
// both spend the last credit) and then either committed on success or
// refunded if the agent fails.
type Reservation struct {
	ID        string    `json:"id" bson:"_id"`
	UserID    string    `json:"userId" bson:"userId"`
	Amount    int64     `json:"amount" bson:"amount"`
	Reason    string    `json:"reason" bson:"reason"`
	Status    string    `json:"status" bson:"status"` // reserved | committed | refunded
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updatedAt"`
}

// Reservation statuses.
const (
	StatusReserved  = "reserved"
	StatusCommitted = "committed"
	StatusRefunded  = "refunded"
)

// Grant records credits added by a purchase. Its ID is the idempotency key
// (the Razorpay order id), so the same purchase can never be credited twice.
type Grant struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"userId"`
	Amount    int64     `bson:"amount"`
	Plan      string    `bson:"plan"`
	Applied   bool      `bson:"applied"`
	CreatedAt time.Time `bson:"createdAt"`
}

// Store errors.
var (
	ErrUserNotFound        = errors.New("user not found")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrReservationSettled  = errors.New("reservation already settled")
)

// Store persists users and credit movements.
type Store interface {
	// UpsertUser finds the user for a Firebase identity or creates one with
	// the starting credit balance. Profile fields are refreshed on each login.
	UpsertUser(ctx context.Context, id Identity, startingCredits int64) (User, error)
	GetUser(ctx context.Context, userID string) (User, error)

	// Reserve atomically subtracts amount if the balance allows it.
	Reserve(ctx context.Context, userID string, amount int64, reason string) (Reservation, int64, error)
	// Commit marks a reservation as spent. Idempotent.
	Commit(ctx context.Context, reservationID string) error
	// Refund returns a reservation's credits. Idempotent: refunding twice
	// credits the user once.
	Refund(ctx context.Context, reservationID string) (int64, error)

	// Grant adds purchased credits once per key and extends the plan.
	// It returns false if the key was already applied.
	Grant(ctx context.Context, key, userID string, amount int64, plan string, validity time.Duration) (bool, error)
}
