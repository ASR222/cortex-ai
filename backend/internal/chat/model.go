// Package chat stores conversations and messages.
//
// Ownership is enforced in the storage layer: every query filters by userId
// as well as the document id, so there is no code path that can read or
// modify another user's conversation even if a handler forgets a check.
package chat

import (
	"context"
	"errors"
	"time"
)

// DefaultTitle is the title of a conversation before its first message.
const DefaultTitle = "New Chat"

// Conversation is one chat thread.
type Conversation struct {
	ID        string     `json:"_id" bson:"_id"`
	UserID    string     `json:"userId" bson:"userId"`
	Title     string     `json:"title" bson:"title"`
	Documents []Document `json:"documents" bson:"documents"`
	CreatedAt time.Time  `json:"createdAt" bson:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt" bson:"updatedAt"`
}

// Document is a PDF the user uploaded and the agent indexed for RAG.
type Document struct {
	FileID    string    `json:"fileId" bson:"fileId"`
	Name      string    `json:"name" bson:"name"`
	Pages     int       `json:"pages" bson:"pages"`
	Chunks    int       `json:"chunks" bson:"chunks"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
}

// Message is one turn in a conversation.
type Message struct {
	ID             string       `json:"_id" bson:"_id"`
	ConversationID string       `json:"conversationId" bson:"conversationId"`
	UserID         string       `json:"-" bson:"userId"`
	Role           string       `json:"role" bson:"role"`
	Content        string       `json:"content" bson:"content"`
	Agent          string       `json:"agent,omitempty" bson:"agent,omitempty"`
	Artifacts      []Artifact   `json:"artifacts" bson:"artifacts"`
	Attachments    []Attachment `json:"attachments" bson:"attachments"`
	Images         []string     `json:"images" bson:"images"`
	Sources        []Source     `json:"sources" bson:"sources"`
	CreatedAt      time.Time    `json:"createdAt" bson:"createdAt"`
}

// Artifact is a generated code project shown in the side panel.
type Artifact struct {
	ID        string         `json:"id" bson:"id"`
	Type      string         `json:"type" bson:"type"`
	Title     string         `json:"title" bson:"title"`
	Files     []ArtifactFile `json:"files" bson:"files"`
	CreatedAt time.Time      `json:"createdAt" bson:"createdAt"`
}

// ArtifactFile is one file inside an Artifact.
type ArtifactFile struct {
	Name    string `json:"name" bson:"name"`
	Content string `json:"content" bson:"content"`
}

// Attachment points at a stored file (an upload or a generated PDF/PPT/image).
// The file itself lives in object storage; FileID is its storage key.
type Attachment struct {
	FileID string `json:"fileId" bson:"fileId"`
	Name   string `json:"name" bson:"name"`
	Mime   string `json:"mime" bson:"mime"`
	Kind   string `json:"kind" bson:"kind"` // upload | generated
}

// Source is a web or document citation.
type Source struct {
	Title string `json:"title" bson:"title"`
	URL   string `json:"url" bson:"url"`
}

// ErrNotFound means the conversation does not exist or belongs to someone else.
// The two cases are deliberately indistinguishable to avoid leaking ids.
var ErrNotFound = errors.New("conversation not found")

// Store persists conversations and messages.
type Store interface {
	CreateConversation(ctx context.Context, userID, title string) (Conversation, error)
	ListConversations(ctx context.Context, userID string, limit int) ([]Conversation, error)
	GetConversation(ctx context.Context, userID, id string) (Conversation, error)
	RenameConversation(ctx context.Context, userID, id, title string) (Conversation, error)
	DeleteConversation(ctx context.Context, userID, id string) error

	// ListMessages returns the newest limit messages in chronological order.
	ListMessages(ctx context.Context, userID, conversationID string, limit int) ([]Message, error)
	// AppendMessages stores msgs and, if the conversation still has the
	// default title, names it after the first user message.
	AppendMessages(ctx context.Context, userID, conversationID string, msgs []Message) ([]Message, error)
	AddDocument(ctx context.Context, userID, conversationID string, doc Document) error
}
