// Command auth runs the auth service: users, sign-in verification and credits.
package main

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/auth"
	"cortexai/internal/platform/config"
	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
	"cortexai/internal/platform/mongox"
	"cortexai/internal/platform/server"
)

func main() {
	server.HandleHealthcheckCommand(":8081")
	server.SetupLogger("auth")

	var cfg config.Loader
	addr := cfg.String("ADDR", ":8081")
	mongoURI := cfg.Required("MONGODB_URI")
	dbName := cfg.String("MONGODB_DB", "cortexai")
	internalToken := cfg.Required("INTERNAL_TOKEN")
	projectID := cfg.Required("FIREBASE_PROJECT_ID")
	startingCredits := cfg.Int("STARTING_CREDITS", 50)
	if err := cfg.Err(); err != nil {
		server.Fatal("invalid configuration", err)
	}

	ctx := context.Background()
	client, err := mongox.Connect(ctx, mongoURI)
	if err != nil {
		server.Fatal("mongo", err)
	}
	store, err := auth.NewMongoStore(ctx, client.Database(dbName))
	if err != nil {
		server.Fatal("store", err)
	}

	h := &auth.Handler{
		Store:           store,
		Verifier:        auth.NewFirebaseVerifier(projectID),
		StartingCredits: int64(startingCredits),
		Now:             time.Now,
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware(true), httpx.Logger, httpx.Recoverer, internalauth.Require(internalToken))
	r.Get("/healthz", httpx.Healthz)
	h.Routes(r)

	server.Run(addr, r, func(ctx context.Context) { _ = client.Disconnect(ctx) })
}
