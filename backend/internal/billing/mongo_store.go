package billing

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
type MongoStore struct {
	payments *mongo.Collection
	now      func() time.Time
}

// NewMongoStore returns a store and ensures indexes exist.
func NewMongoStore(ctx context.Context, db *mongo.Database) (*MongoStore, error) {
	s := &MongoStore{payments: db.Collection("payments"), now: time.Now}
	_, err := s.payments.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "orderId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("payments index: %w", err)
	}
	return s, nil
}

func (s *MongoStore) Create(ctx context.Context, p Payment) error {
	now := s.now()
	p.ID = bson.NewObjectID().Hex()
	p.CreatedAt, p.UpdatedAt = now, now
	if _, err := s.payments.InsertOne(ctx, p); err != nil {
		return fmt.Errorf("create payment: %w", err)
	}
	return nil
}

func (s *MongoStore) GetByOrder(ctx context.Context, orderID string) (Payment, error) {
	var p Payment
	err := s.payments.FindOne(ctx, bson.M{"orderId": orderID}).Decode(&p)
	if mongox.IsNotFound(err) {
		return Payment{}, ErrPaymentNotFound
	}
	if err != nil {
		return Payment{}, fmt.Errorf("get payment: %w", err)
	}
	return p, nil
}

func (s *MongoStore) MarkPaid(ctx context.Context, orderID, paymentID string) (Payment, error) {
	var p Payment
	err := s.payments.FindOneAndUpdate(ctx,
		bson.M{"orderId": orderID, "status": bson.M{"$ne": StatusPaid}},
		bson.M{"$set": bson.M{"status": StatusPaid, "paymentId": paymentID, "updatedAt": s.now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&p)
	if mongox.IsNotFound(err) {
		// Already paid (or unknown): return the current state.
		return s.GetByOrder(ctx, orderID)
	}
	if err != nil {
		return Payment{}, fmt.Errorf("mark paid: %w", err)
	}
	return p, nil
}

func (s *MongoStore) MarkFailed(ctx context.Context, orderID string) error {
	_, err := s.payments.UpdateOne(ctx,
		bson.M{"orderId": orderID, "status": StatusCreated},
		bson.M{"$set": bson.M{"status": StatusFailed, "updatedAt": s.now()}},
	)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

func (s *MongoStore) ListByUser(ctx context.Context, userID string, limit int) ([]Payment, error) {
	cur, err := s.payments.Find(ctx, bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	out := []Payment{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	return out, nil
}
