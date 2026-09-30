// Command billing runs the billing service: Razorpay orders and credit purchases.
package main

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/billing"
	"cortexai/internal/platform/config"
	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
	"cortexai/internal/platform/mongox"
	"cortexai/internal/platform/server"
)

// authCredits grants credits through the auth service.
type authCredits struct{ client *internalauth.Client }

func (a authCredits) Grant(ctx context.Context, key, userID string, amount int64, plan string, validityDays int) error {
	return a.client.Do(ctx, http.MethodPost, "/internal/credits/grant", map[string]any{
		"key":          key,
		"userId":       userID,
		"amount":       amount,
		"plan":         plan,
		"validityDays": validityDays,
	}, nil)
}

func main() {
	server.HandleHealthcheckCommand(":8083")
	server.SetupLogger("billing")

	var cfg config.Loader
	addr := cfg.String("ADDR", ":8083")
	mongoURI := cfg.Required("MONGODB_URI")
	dbName := cfg.String("MONGODB_DB", "cortexai")
	internalToken := cfg.Required("INTERNAL_TOKEN")
	authURL := cfg.Required("AUTH_SERVICE_URL")
	keyID := cfg.Required("RAZORPAY_KEY_ID")
	keySecret := cfg.Required("RAZORPAY_KEY_SECRET")
	webhookSecret := cfg.String("RAZORPAY_WEBHOOK_SECRET", "")
	if err := cfg.Err(); err != nil {
		server.Fatal("invalid configuration", err)
	}

	ctx := context.Background()
	client, err := mongox.Connect(ctx, mongoURI)
	if err != nil {
		server.Fatal("mongo", err)
	}
	store, err := billing.NewMongoStore(ctx, client.Database(dbName))
	if err != nil {
		server.Fatal("store", err)
	}

	h := &billing.Handler{
		Store:         store,
		Gateway:       billing.NewRazorpay(keyID, keySecret),
		Credits:       authCredits{client: internalauth.NewClient(authURL, internalToken)},
		KeyID:         keyID,
		KeySecret:     keySecret,
		WebhookSecret: webhookSecret,
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware(true), httpx.Logger, httpx.Recoverer, internalauth.Require(internalToken))
	r.Get("/healthz", httpx.Healthz)
	h.Routes(r)

	server.Run(addr, r, func(ctx context.Context) { _ = client.Disconnect(ctx) })
}
