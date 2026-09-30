// Command chat runs the chat service: conversations and message history.
package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/chat"
	"cortexai/internal/platform/config"
	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
	"cortexai/internal/platform/mongox"
	"cortexai/internal/platform/server"
)

// agentCleaner asks the agent service to delete a conversation's vectors and files.
type agentCleaner struct{ client *internalauth.Client }

func (a agentCleaner) CleanupConversation(ctx context.Context, userID, conversationID string) error {
	path := "/internal/users/" + url.PathEscape(userID) + "/conversations/" + url.PathEscape(conversationID)
	return a.client.Do(ctx, http.MethodDelete, path, nil, nil)
}

func main() {
	server.HandleHealthcheckCommand(":8082")
	server.SetupLogger("chat")

	var cfg config.Loader
	addr := cfg.String("ADDR", ":8082")
	mongoURI := cfg.Required("MONGODB_URI")
	dbName := cfg.String("MONGODB_DB", "cortexai")
	internalToken := cfg.Required("INTERNAL_TOKEN")
	agentURL := cfg.Required("AGENT_SERVICE_URL")
	if err := cfg.Err(); err != nil {
		server.Fatal("invalid configuration", err)
	}

	ctx := context.Background()
	client, err := mongox.Connect(ctx, mongoURI)
	if err != nil {
		server.Fatal("mongo", err)
	}
	store, err := chat.NewMongoStore(ctx, client.Database(dbName))
	if err != nil {
		server.Fatal("store", err)
	}

	h := &chat.Handler{
		Store:   store,
		Cleaner: agentCleaner{client: internalauth.NewClient(agentURL, internalToken)},
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware(true), httpx.Logger, httpx.Recoverer, internalauth.Require(internalToken))
	r.Get("/healthz", httpx.Healthz)
	h.Routes(r)

	server.Run(addr, r, func(ctx context.Context) { _ = client.Disconnect(ctx) })
}
