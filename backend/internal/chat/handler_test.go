package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"cortexai/internal/platform/httpx"
)

// memStore is an in-memory Store with the same ownership rules as MongoStore.
type memStore struct {
	mu    sync.Mutex
	next  int
	convs map[string]Conversation
	msgs  map[string][]Message
}

func newMemStore() *memStore {
	return &memStore{convs: map[string]Conversation{}, msgs: map[string][]Message{}}
}

func (m *memStore) id() string { m.next++; return fmt.Sprintf("%024x", m.next) }

func (m *memStore) CreateConversation(_ context.Context, userID, title string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := Conversation{ID: m.id(), UserID: userID, Title: title, Documents: []Document{}}
	m.convs[c.ID] = c
	return c, nil
}

func (m *memStore) ListConversations(_ context.Context, userID string, _ int) ([]Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Conversation{}
	for _, c := range m.convs {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memStore) GetConversation(_ context.Context, userID, id string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok || c.UserID != userID {
		return Conversation{}, ErrNotFound
	}
	return c, nil
}

func (m *memStore) RenameConversation(ctx context.Context, userID, id, title string) (Conversation, error) {
	c, err := m.GetConversation(ctx, userID, id)
	if err != nil {
		return c, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c.Title = title
	m.convs[id] = c
	return c, nil
}

func (m *memStore) DeleteConversation(ctx context.Context, userID, id string) error {
	if _, err := m.GetConversation(ctx, userID, id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.convs, id)
	delete(m.msgs, id)
	return nil
}

func (m *memStore) ListMessages(_ context.Context, _ string, id string, _ int) ([]Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message{}, m.msgs[id]...), nil
}

func (m *memStore) AppendMessages(ctx context.Context, userID, id string, msgs []Message) ([]Message, error) {
	c, err := m.GetConversation(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs[id] = append(m.msgs[id], msgs...)
	if c.Title == DefaultTitle {
		c.Title = TitleFrom(msgs[0].Content)
		m.convs[id] = c
	}
	return msgs, nil
}

func (m *memStore) AddDocument(ctx context.Context, userID, id string, doc Document) error {
	c, err := m.GetConversation(ctx, userID, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c.Documents = append(c.Documents, doc)
	m.convs[id] = c
	return nil
}

type cleaner struct {
	mu    sync.Mutex
	calls []string
	done  chan struct{}
}

func (c *cleaner) CleanupConversation(_ context.Context, userID, id string) error {
	c.mu.Lock()
	c.calls = append(c.calls, userID+"/"+id)
	c.mu.Unlock()
	close(c.done)
	return nil
}

func newServer() (*memStore, *cleaner, http.Handler) {
	s := newMemStore()
	cl := &cleaner{done: make(chan struct{})}
	r := chi.NewRouter()
	(&Handler{Store: s, Cleaner: cl}).Routes(r)
	return s, cl, r
}

func call(t *testing.T, h http.Handler, method, path, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	if userID != "" {
		req.Header.Set(httpx.HeaderUserID, userID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUsersCannotSeeEachOthersConversations(t *testing.T) {
	s, _, h := newServer()
	alice, _ := s.CreateConversation(context.Background(), "alice", DefaultTitle)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/conversations/" + alice.ID + "/messages"},
		{http.MethodPatch, "/conversations/" + alice.ID},
		{http.MethodDelete, "/conversations/" + alice.ID},
	} {
		rec := call(t, h, tc.method, tc.path, "mallory", map[string]string{"title": "pwned"})
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s", tc.method, tc.path)
	}
	got, err := s.GetConversation(context.Background(), "alice", alice.ID)
	require.NoError(t, err)
	require.Equal(t, DefaultTitle, got.Title)
}

func TestRequiresUser(t *testing.T) {
	_, _, h := newServer()
	require.Equal(t, http.StatusUnauthorized, call(t, h, http.MethodGet, "/conversations", "", nil).Code)
}

func TestCreateRenameList(t *testing.T) {
	_, _, h := newServer()
	rec := call(t, h, http.MethodPost, "/conversations", "alice", nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	var c Conversation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &c))
	require.Equal(t, DefaultTitle, c.Title)

	rec = call(t, h, http.MethodPatch, "/conversations/"+c.ID, "alice", map[string]string{"title": "  Go   tips "})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"title":"Go tips"`)

	rec = call(t, h, http.MethodPatch, "/conversations/"+c.ID, "alice", map[string]string{"title": "   "})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestInternalAppendValidatesAttachmentsOwner(t *testing.T) {
	s, _, h := newServer()
	c, _ := s.CreateConversation(context.Background(), "alice", DefaultTitle)
	path := "/internal/users/alice/conversations/" + c.ID + "/messages"

	rec := call(t, h, http.MethodPost, path, "", map[string]any{"messages": []Message{{
		Role: "assistant", Content: "here", Attachments: []Attachment{{FileID: "bob/x/y.pdf", Name: "y.pdf"}},
	}}})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = call(t, h, http.MethodPost, path, "", map[string]any{"messages": []Message{
		{Role: "user", Content: "Explain goroutines please"},
		{Role: "assistant", Content: "Sure", Attachments: []Attachment{{FileID: "alice/" + c.ID + "/a.pdf", Name: "a.pdf"}}},
	}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got, _ := s.GetConversation(context.Background(), "alice", c.ID)
	require.Equal(t, "Explain goroutines please", got.Title)

	rec = call(t, h, http.MethodPost, path, "", map[string]any{"messages": []Message{{Role: "system", Content: "x"}}})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteTriggersCleanup(t *testing.T) {
	s, cl, h := newServer()
	c, _ := s.CreateConversation(context.Background(), "alice", DefaultTitle)
	rec := call(t, h, http.MethodDelete, "/conversations/"+c.ID, "alice", nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	<-cl.done
	require.Equal(t, []string{"alice/" + c.ID}, cl.calls)
}

func TestInvalidIDIs404(t *testing.T) {
	_, _, h := newServer()
	rec := call(t, h, http.MethodGet, "/conversations/not-an-id/messages", "alice", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTitleFrom(t *testing.T) {
	require.Equal(t, "hello world", TitleFrom("  hello\n\nworld "))
	long := TitleFrom(string(bytes.Repeat([]byte("a"), 100)))
	require.Equal(t, 61, len([]rune(long)))
}
