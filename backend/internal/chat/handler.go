package chat

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
)

// Cleaner removes data other services keep for a conversation (vectors and
// generated files in the agent service).
type Cleaner interface {
	CleanupConversation(ctx context.Context, userID, conversationID string) error
}

// Handler serves the chat API.
type Handler struct {
	Store   Store
	Cleaner Cleaner
}

// Routes registers the handler's endpoints.
//
// Public routes (reached through the gateway at /api/chat/...) take the user
// from the X-User-Id header. Internal routes take it from the path because
// they are called by the agent service on the user's behalf; the gateway
// refuses to forward any /internal path.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/conversations", httpx.Handle(h.listConversations))
	r.Post("/conversations", httpx.Handle(h.createConversation))
	r.Patch("/conversations/{id}", httpx.Handle(h.renameConversation))
	r.Delete("/conversations/{id}", httpx.Handle(h.deleteConversation))
	r.Get("/conversations/{id}/messages", httpx.Handle(h.listMessages))

	r.Route("/internal/users/{userId}/conversations/{id}", func(r chi.Router) {
		r.Get("/", httpx.Handle(h.internalGetConversation))
		r.Get("/messages", httpx.Handle(h.internalListMessages))
		r.Post("/messages", httpx.Handle(h.internalAppendMessages))
		r.Post("/documents", httpx.Handle(h.internalAddDocument))
	})
}

func notFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return httpx.NewError(http.StatusNotFound, "not_found", "Conversation not found.")
	}
	return err
}

func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func conversationID(r *http.Request) (string, error) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		return "", httpx.NewError(http.StatusNotFound, "not_found", "Conversation not found.")
	}
	return id, nil
}

func cleanTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return "", httpx.BadRequest("Title cannot be empty.")
	}
	if utf8.RuneCountInString(t) > 100 {
		return "", httpx.BadRequest("Title must be at most 100 characters.")
	}
	return t, nil
}

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	convs, err := h.Store.ListConversations(r.Context(), userID, 200)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, convs)
	return nil
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	var in struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(w, r, &in, 4<<10); err != nil {
			return err
		}
	}
	title := DefaultTitle
	if strings.TrimSpace(in.Title) != "" {
		if title, err = cleanTitle(in.Title); err != nil {
			return err
		}
	}
	c, err := h.Store.CreateConversation(r.Context(), userID, title)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
	return nil
}

func (h *Handler) renameConversation(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	id, err := conversationID(r)
	if err != nil {
		return err
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 4<<10); err != nil {
		return err
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return err
	}
	c, err := h.Store.RenameConversation(r.Context(), userID, id, title)
	if err != nil {
		return notFound(err)
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	id, err := conversationID(r)
	if err != nil {
		return err
	}
	if err := h.Store.DeleteConversation(r.Context(), userID, id); err != nil {
		return notFound(err)
	}
	if h.Cleaner != nil {
		// Vector and file cleanup is best effort and must not block or fail
		// the user's delete; it runs detached from the request context.
		reqID := httpx.RequestID(r.Context())
		go func() {
			ctx, cancel := context.WithTimeout(httpx.WithRequestID(context.Background(), reqID), 30*time.Second)
			defer cancel()
			if err := h.Cleaner.CleanupConversation(ctx, userID, id); err != nil {
				slog.WarnContext(ctx, "conversation cleanup failed", "conversation", id, "err", err)
			}
		}()
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) error {
	userID, err := internalauth.UserID(r)
	if err != nil {
		return err
	}
	id, err := conversationID(r)
	if err != nil {
		return err
	}
	if _, err := h.Store.GetConversation(r.Context(), userID, id); err != nil {
		return notFound(err)
	}
	msgs, err := h.Store.ListMessages(r.Context(), userID, id, 500)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

// --- internal routes (agent service) ---

func internalIDs(r *http.Request) (string, string, error) {
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		return "", "", httpx.ErrUnauthorized
	}
	id, err := conversationID(r)
	return userID, id, err
}

func (h *Handler) internalGetConversation(w http.ResponseWriter, r *http.Request) error {
	userID, id, err := internalIDs(r)
	if err != nil {
		return err
	}
	c, err := h.Store.GetConversation(r.Context(), userID, id)
	if err != nil {
		return notFound(err)
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (h *Handler) internalListMessages(w http.ResponseWriter, r *http.Request) error {
	userID, id, err := internalIDs(r)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if _, err := h.Store.GetConversation(r.Context(), userID, id); err != nil {
		return notFound(err)
	}
	msgs, err := h.Store.ListMessages(r.Context(), userID, id, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

const (
	maxContent       = 200_000
	maxArtifactFiles = 30
)

func validateMessage(userID string, m Message) error {
	if m.Role != "user" && m.Role != "assistant" {
		return httpx.BadRequest("role must be user or assistant.")
	}
	if len(m.Content) > maxContent {
		return httpx.BadRequest("Message is too long.")
	}
	for _, a := range m.Artifacts {
		if len(a.Files) > maxArtifactFiles {
			return httpx.BadRequest("Too many artifact files.")
		}
		for _, f := range a.Files {
			if len(f.Content) > maxContent {
				return httpx.BadRequest("Artifact file is too large.")
			}
		}
	}
	for _, a := range m.Attachments {
		// Storage keys are namespaced by owner; a message can only reference
		// its own user's files.
		if !strings.HasPrefix(a.FileID, userID+"/") {
			return httpx.BadRequest("Attachment does not belong to this user.")
		}
	}
	return nil
}

func (h *Handler) internalAppendMessages(w http.ResponseWriter, r *http.Request) error {
	userID, id, err := internalIDs(r)
	if err != nil {
		return err
	}
	var in struct {
		Messages []Message `json:"messages"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 8<<20); err != nil {
		return err
	}
	if len(in.Messages) == 0 || len(in.Messages) > 10 {
		return httpx.BadRequest("Provide between 1 and 10 messages.")
	}
	for _, m := range in.Messages {
		if err := validateMessage(userID, m); err != nil {
			return err
		}
	}
	saved, err := h.Store.AppendMessages(r.Context(), userID, id, in.Messages)
	if err != nil {
		return notFound(err)
	}
	httpx.WriteJSON(w, http.StatusCreated, saved)
	return nil
}

func (h *Handler) internalAddDocument(w http.ResponseWriter, r *http.Request) error {
	userID, id, err := internalIDs(r)
	if err != nil {
		return err
	}
	var doc Document
	if err := httpx.DecodeJSON(w, r, &doc, 16<<10); err != nil {
		return err
	}
	if doc.FileID == "" || doc.Name == "" || !strings.HasPrefix(doc.FileID, userID+"/") {
		return httpx.BadRequest("A valid fileId and name are required.")
	}
	if err := h.Store.AddDocument(r.Context(), userID, id, doc); err != nil {
		return notFound(err)
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true})
	return nil
}
