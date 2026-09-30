package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"cortexai/internal/platform/mongox"
)

// MongoStore implements Store on MongoDB.
type MongoStore struct {
	conversations *mongo.Collection
	messages      *mongo.Collection
	now           func() time.Time
}

// NewMongoStore returns a store and ensures indexes exist.
func NewMongoStore(ctx context.Context, db *mongo.Database) (*MongoStore, error) {
	s := &MongoStore{
		conversations: db.Collection("conversations"),
		messages:      db.Collection("messages"),
		now:           time.Now,
	}
	if _, err := s.conversations.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "userId", Value: 1}, {Key: "updatedAt", Value: -1}},
	}); err != nil {
		return nil, fmt.Errorf("conversations index: %w", err)
	}
	if _, err := s.messages.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "conversationId", Value: 1}, {Key: "createdAt", Value: 1}},
	}); err != nil {
		return nil, fmt.Errorf("messages index: %w", err)
	}
	return s, nil
}

func (s *MongoStore) CreateConversation(ctx context.Context, userID, title string) (Conversation, error) {
	now := s.now()
	c := Conversation{
		ID:        bson.NewObjectID().Hex(),
		UserID:    userID,
		Title:     title,
		Documents: []Document{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := s.conversations.InsertOne(ctx, c); err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return c, nil
}

func (s *MongoStore) ListConversations(ctx context.Context, userID string, limit int) ([]Conversation, error) {
	cur, err := s.conversations.Find(ctx, bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	out := []Conversation{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return out, nil
}

func (s *MongoStore) GetConversation(ctx context.Context, userID, id string) (Conversation, error) {
	var c Conversation
	err := s.conversations.FindOne(ctx, bson.M{"_id": id, "userId": userID}).Decode(&c)
	if mongox.IsNotFound(err) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	if c.Documents == nil {
		c.Documents = []Document{}
	}
	return c, nil
}

func (s *MongoStore) RenameConversation(ctx context.Context, userID, id, title string) (Conversation, error) {
	var c Conversation
	err := s.conversations.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "userId": userID},
		bson.M{"$set": bson.M{"title": title, "updatedAt": s.now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&c)
	if mongox.IsNotFound(err) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("rename conversation: %w", err)
	}
	return c, nil
}

func (s *MongoStore) DeleteConversation(ctx context.Context, userID, id string) error {
	res, err := s.conversations.DeleteOne(ctx, bson.M{"_id": id, "userId": userID})
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	if _, err := s.messages.DeleteMany(ctx, bson.M{"conversationId": id, "userId": userID}); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return nil
}

func (s *MongoStore) ListMessages(ctx context.Context, userID, conversationID string, limit int) ([]Message, error) {
	// Newest first with a limit, then reversed into chronological order.
	cur, err := s.messages.Find(ctx,
		bson.M{"conversationId": conversationID, "userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	out := []Message{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	slices.Reverse(out)
	for i := range out {
		normalize(&out[i])
	}
	return out, nil
}

func (s *MongoStore) AppendMessages(ctx context.Context, userID, conversationID string, msgs []Message) ([]Message, error) {
	conv, err := s.GetConversation(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	docs := make([]any, len(msgs))
	for i := range msgs {
		msgs[i].ID = bson.NewObjectID().Hex()
		msgs[i].ConversationID = conversationID
		msgs[i].UserID = userID
		// Distinct timestamps keep user/assistant order stable when sorting.
		msgs[i].CreatedAt = now.Add(time.Duration(i) * time.Millisecond)
		normalize(&msgs[i])
		docs[i] = msgs[i]
	}
	if _, err := s.messages.InsertMany(ctx, docs); err != nil {
		return nil, fmt.Errorf("append messages: %w", err)
	}

	set := bson.M{"updatedAt": now}
	if conv.Title == DefaultTitle {
		for _, m := range msgs {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				set["title"] = TitleFrom(m.Content)
				break
			}
		}
	}
	if _, err := s.conversations.UpdateOne(ctx, bson.M{"_id": conversationID, "userId": userID}, bson.M{"$set": set}); err != nil {
		return nil, fmt.Errorf("touch conversation: %w", err)
	}
	return msgs, nil
}

func (s *MongoStore) AddDocument(ctx context.Context, userID, conversationID string, doc Document) error {
	doc.CreatedAt = s.now()
	res, err := s.conversations.UpdateOne(ctx,
		bson.M{"_id": conversationID, "userId": userID},
		bson.M{"$push": bson.M{"documents": doc}, "$set": bson.M{"updatedAt": doc.CreatedAt}},
	)
	if err != nil {
		return fmt.Errorf("add document: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// normalize replaces nil slices so the JSON always has arrays, never null.
func normalize(m *Message) {
	if m.Artifacts == nil {
		m.Artifacts = []Artifact{}
	}
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	if m.Images == nil {
		m.Images = []string{}
	}
	if m.Sources == nil {
		m.Sources = []Source{}
	}
}

// TitleFrom derives a conversation title from the first user message.
func TitleFrom(content string) string {
	t := strings.Join(strings.Fields(content), " ")
	const max = 60
	if utf8.RuneCountInString(t) <= max {
		return t
	}
	r := []rune(t)
	return strings.TrimSpace(string(r[:max])) + "…"
}
