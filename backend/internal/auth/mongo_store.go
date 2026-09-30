package auth

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"cortexai/internal/platform/mongox"
)

// MongoStore implements Store on MongoDB.
//
// Every balance change is a single-document atomic update with a guard in the
// filter (for example {credits: {$gte: amount}}). That makes double-spending
// impossible without needing multi-document transactions. Where two writes
// are unavoidable (reservation record + balance), they are ordered so a crash
// in between can only ever cost the user a reservation, never mint credits.
type MongoStore struct {
	users        *mongo.Collection
	reservations *mongo.Collection
	grants       *mongo.Collection
	now          func() time.Time
}

// NewMongoStore returns a store and ensures indexes exist.
func NewMongoStore(ctx context.Context, db *mongo.Database) (*MongoStore, error) {
	s := &MongoStore{
		users:        db.Collection("users"),
		reservations: db.Collection("credit_reservations"),
		grants:       db.Collection("credit_grants"),
		now:          time.Now,
	}
	_, err := s.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "firebaseUid", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("users index: %w", err)
	}
	_, err = s.reservations.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
	})
	if err != nil {
		return nil, fmt.Errorf("reservations index: %w", err)
	}
	return s, nil
}

func (s *MongoStore) UpsertUser(ctx context.Context, id Identity, startingCredits int64) (User, error) {
	now := s.now()
	filter := bson.M{"firebaseUid": id.UID}
	update := bson.M{
		"$set": bson.M{
			"email":     id.Email,
			"name":      id.Name,
			"avatar":    id.Picture,
			"provider":  id.Provider,
			"updatedAt": now,
		},
		"$setOnInsert": bson.M{
			"_id":          bson.NewObjectID().Hex(),
			"plan":         "free",
			"credits":      startingCredits,
			"totalCredits": startingCredits,
			"createdAt":    now,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var u User
	err := s.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&u)
	if mongo.IsDuplicateKeyError(err) {
		// Two first-time logins raced; the other one created the user.
		err = s.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&u)
	}
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

func (s *MongoStore) GetUser(ctx context.Context, userID string) (User, error) {
	var u User
	err := s.users.FindOne(ctx, bson.M{"_id": userID}).Decode(&u)
	if mongox.IsNotFound(err) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *MongoStore) Reserve(ctx context.Context, userID string, amount int64, reason string) (Reservation, int64, error) {
	if amount <= 0 {
		return Reservation{}, 0, fmt.Errorf("reserve: amount must be positive")
	}
	now := s.now()

	// The $gte guard makes check-and-subtract one atomic operation, so
	// concurrent requests can never drive the balance below zero.
	var u User
	err := s.users.FindOneAndUpdate(ctx,
		bson.M{"_id": userID, "credits": bson.M{"$gte": amount}},
		bson.M{"$inc": bson.M{"credits": -amount}, "$set": bson.M{"updatedAt": now}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&u)
	if mongox.IsNotFound(err) {
		if _, getErr := s.GetUser(ctx, userID); getErr != nil {
			return Reservation{}, 0, getErr
		}
		return Reservation{}, 0, ErrInsufficientCredits
	}
	if err != nil {
		return Reservation{}, 0, fmt.Errorf("reserve: %w", err)
	}

	r := Reservation{
		ID:        bson.NewObjectID().Hex(),
		UserID:    userID,
		Amount:    amount,
		Reason:    reason,
		Status:    StatusReserved,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := s.reservations.InsertOne(ctx, r); err != nil {
		// Give the credits back rather than leave an untracked deduction.
		_, _ = s.users.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{"$inc": bson.M{"credits": amount}})
		return Reservation{}, 0, fmt.Errorf("record reservation: %w", err)
	}
	return r, u.Credits, nil
}

// settle moves a reservation out of "reserved". It returns the reservation
// and whether this call performed the transition.
func (s *MongoStore) settle(ctx context.Context, reservationID, status string) (Reservation, bool, error) {
	var r Reservation
	err := s.reservations.FindOneAndUpdate(ctx,
		bson.M{"_id": reservationID, "status": StatusReserved},
		bson.M{"$set": bson.M{"status": status, "updatedAt": s.now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&r)
	if err == nil {
		return r, true, nil
	}
	if !mongox.IsNotFound(err) {
		return Reservation{}, false, fmt.Errorf("settle: %w", err)
	}
	// Not in "reserved": either unknown or already settled.
	if err := s.reservations.FindOne(ctx, bson.M{"_id": reservationID}).Decode(&r); err != nil {
		if mongox.IsNotFound(err) {
			return Reservation{}, false, ErrReservationNotFound
		}
		return Reservation{}, false, fmt.Errorf("settle lookup: %w", err)
	}
	return r, false, nil
}

func (s *MongoStore) Commit(ctx context.Context, reservationID string) error {
	r, changed, err := s.settle(ctx, reservationID, StatusCommitted)
	if err != nil {
		return err
	}
	if !changed && r.Status != StatusCommitted {
		return ErrReservationSettled
	}
	return nil
}

func (s *MongoStore) Refund(ctx context.Context, reservationID string) (int64, error) {
	r, changed, err := s.settle(ctx, reservationID, StatusRefunded)
	if err != nil {
		return 0, err
	}
	if !changed {
		if r.Status == StatusRefunded {
			u, err := s.GetUser(ctx, r.UserID)
			return u.Credits, err
		}
		return 0, ErrReservationSettled
	}
	// Only the caller that flipped the status gives the credits back, so a
	// retried refund cannot pay out twice.
	var u User
	err = s.users.FindOneAndUpdate(ctx,
		bson.M{"_id": r.UserID},
		bson.M{"$inc": bson.M{"credits": r.Amount}, "$set": bson.M{"updatedAt": s.now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&u)
	if err != nil {
		return 0, fmt.Errorf("refund credits: %w", err)
	}
	return u.Credits, nil
}

func (s *MongoStore) Grant(ctx context.Context, key, userID string, amount int64, plan string, validity time.Duration) (bool, error) {
	now := s.now()
	expires := now.Add(validity)

	// The grant key is pushed onto the user in the same atomic update that
	// adds the credits. The {$ne: key} filter means a replayed webhook or a
	// retried verify call matches nothing and changes nothing.
	res, err := s.users.UpdateOne(ctx,
		bson.M{"_id": userID, "appliedGrants": bson.M{"$ne": key}},
		bson.M{
			"$inc":  bson.M{"credits": amount, "totalCredits": amount},
			"$set":  bson.M{"plan": plan, "planExpiresAt": expires, "updatedAt": now},
			"$push": bson.M{"appliedGrants": key},
		},
	)
	if err != nil {
		return false, fmt.Errorf("grant: %w", err)
	}
	if res.MatchedCount == 0 {
		if _, err := s.GetUser(ctx, userID); err != nil {
			return false, err
		}
		return false, nil // already applied
	}

	// Audit record; best effort because the user document is the source of truth.
	_, _ = s.grants.UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$setOnInsert": Grant{
		ID: key, UserID: userID, Amount: amount, Plan: plan, Applied: true, CreatedAt: now,
	}}, options.UpdateOne().SetUpsert(true))
	return true, nil
}
