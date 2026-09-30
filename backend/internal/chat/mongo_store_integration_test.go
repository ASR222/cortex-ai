//go:build integration

package chat

import (
	"context"
	"os"
	"testing"

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
	db := client.Database("cortex_test_chat_" + bson.NewObjectID().Hex())
	t.Cleanup(func() {
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	})
	s, err := NewMongoStore(ctx, db)
	require.NoError(t, err)
	return s
}

func TestMongoOwnershipIsEnforcedInQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, "alice", DefaultTitle)
	require.NoError(t, err)

	_, err = s.GetConversation(ctx, "mallory", c.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = s.RenameConversation(ctx, "mallory", c.ID, "x")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, s.DeleteConversation(ctx, "mallory", c.ID), ErrNotFound)
	_, err = s.AppendMessages(ctx, "mallory", c.ID, []Message{{Role: "user", Content: "hi"}})
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, s.AddDocument(ctx, "mallory", c.ID, Document{FileID: "mallory/x/y", Name: "y"}), ErrNotFound)
}

func TestMongoMessagesOrderAndTitle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateConversation(ctx, "alice", DefaultTitle)

	for i := range 3 {
		_, err := s.AppendMessages(ctx, "alice", c.ID, []Message{
			{Role: "user", Content: "question " + string(rune('A'+i))},
			{Role: "assistant", Content: "answer"},
		})
		require.NoError(t, err)
	}
	msgs, err := s.ListMessages(ctx, "alice", c.ID, 4)
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	require.Equal(t, "question B", msgs[0].Content, "newest 4, in chronological order")
	require.Equal(t, "answer", msgs[3].Content)
	require.NotNil(t, msgs[0].Artifacts)

	got, _ := s.GetConversation(ctx, "alice", c.ID)
	require.Equal(t, "question A", got.Title)

	require.NoError(t, s.DeleteConversation(ctx, "alice", c.ID))
	msgs, _ = s.ListMessages(ctx, "alice", c.ID, 10)
	require.Empty(t, msgs)
}
