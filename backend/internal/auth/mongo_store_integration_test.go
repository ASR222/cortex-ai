//go:build integration

// Integration tests run against a real MongoDB:
//
//	MONGODB_TEST_URI=mongodb://localhost:27017 go test -tags integration ./...
package auth

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"cortexai/internal/platform/mongox"
)

func newTestStore(t *testing.T) *MongoStore {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set")
	}
	ctx := context.Background()
	client, err := mongox.Connect(ctx, uri)
	require.NoError(t, err)
	db := client.Database("cortex_test_auth_" + bson.NewObjectID().Hex())
	t.Cleanup(func() {
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	})
	s, err := NewMongoStore(ctx, db)
	require.NoError(t, err)
	return s
}

func newUser(t *testing.T, s *MongoStore, credits int64) User {
	t.Helper()
	u, err := s.UpsertUser(context.Background(), Identity{UID: bson.NewObjectID().Hex(), Email: "a@b.c"}, credits)
	require.NoError(t, err)
	return u
}

func TestUpsertUserIsStable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, err := s.UpsertUser(ctx, Identity{UID: "uid-1", Name: "Old"}, 50)
	require.NoError(t, err)
	_, _, err = s.Reserve(ctx, a.ID, 10, "chat")
	require.NoError(t, err)

	b, err := s.UpsertUser(ctx, Identity{UID: "uid-1", Name: "New"}, 50)
	require.NoError(t, err)
	require.Equal(t, a.ID, b.ID)
	require.Equal(t, "New", b.Name)
	require.EqualValues(t, 40, b.Credits, "logging in again must not reset credits")
}

func TestConcurrentReservesNeverOverspend(t *testing.T) {
	s := newTestStore(t)
	u := newUser(t, s, 10)

	var ok atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Reserve(context.Background(), u.ID, 1, "chat"); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()

	got, err := s.GetUser(context.Background(), u.ID)
	require.NoError(t, err)
	require.EqualValues(t, 10, ok.Load())
	require.EqualValues(t, 0, got.Credits)
}

func TestRefundIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := newUser(t, s, 20)

	r, bal, err := s.Reserve(ctx, u.ID, 5, "coding")
	require.NoError(t, err)
	require.EqualValues(t, 15, bal)

	for range 3 {
		bal, err = s.Refund(ctx, r.ID)
		require.NoError(t, err)
		require.EqualValues(t, 20, bal)
	}
	require.ErrorIs(t, s.Commit(ctx, r.ID), ErrReservationSettled)
}

func TestCommitThenRefundFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := newUser(t, s, 20)
	r, _, err := s.Reserve(ctx, u.ID, 5, "ppt")
	require.NoError(t, err)
	require.NoError(t, s.Commit(ctx, r.ID))
	require.NoError(t, s.Commit(ctx, r.ID)) // idempotent
	_, err = s.Refund(ctx, r.ID)
	require.ErrorIs(t, err, ErrReservationSettled)
}

func TestInsufficientCredits(t *testing.T) {
	s := newTestStore(t)
	u := newUser(t, s, 3)
	_, _, err := s.Reserve(context.Background(), u.ID, 5, "image")
	require.ErrorIs(t, err, ErrInsufficientCredits)
	_, _, err = s.Reserve(context.Background(), "000000000000000000000000", 1, "chat")
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestGrantAppliesOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := newUser(t, s, 50)

	var applied atomic.Int64
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.Grant(ctx, "order_123", u.ID, 500, "starter", 30*24*time.Hour)
			if err == nil && ok {
				applied.Add(1)
			}
		}()
	}
	wg.Wait()

	got, err := s.GetUser(ctx, u.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, applied.Load())
	require.EqualValues(t, 550, got.Credits)
	require.EqualValues(t, 550, got.TotalCredits)
	require.Equal(t, "starter", got.Plan)
}
