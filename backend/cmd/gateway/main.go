// Command gateway runs the public API gateway.
package main

import (
	"context"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"

	"cortexai/internal/gateway"
	"cortexai/internal/platform/config"
	"cortexai/internal/platform/server"
)

func mustURL(raw, name string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		server.Fatal("invalid "+name, err)
	}
	return u
}

func main() {
	server.HandleHealthcheckCommand(":8080")
	server.SetupLogger("gateway")

	var cfg config.Loader
	addr := cfg.String("ADDR", ":8080")
	redisURL := cfg.Required("REDIS_URL")
	internalToken := cfg.Required("INTERNAL_TOKEN")
	authURL := cfg.Required("AUTH_SERVICE_URL")
	chatURL := cfg.Required("CHAT_SERVICE_URL")
	agentURL := cfg.Required("AGENT_SERVICE_URL")
	billingURL := cfg.Required("BILLING_SERVICE_URL")
	cookieSecure := cfg.Bool("COOKIE_SECURE", true)
	trustProxy := cfg.Bool("TRUST_PROXY", false)
	sessionTTL := cfg.Duration("SESSION_TTL", 7*24*time.Hour)
	apiPerMin := cfg.Int("RATE_LIMIT_API_PER_MIN", 120)
	agentPerMin := cfg.Int("RATE_LIMIT_AGENT_PER_MIN", 20)
	loginPerMin := cfg.Int("RATE_LIMIT_LOGIN_PER_MIN", 10)
	if err := cfg.Err(); err != nil {
		server.Fatal("invalid configuration", err)
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		server.Fatal("invalid REDIS_URL", err)
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		server.Fatal("redis", err)
	}

	g := gateway.New(gateway.Config{
		AuthURL:       mustURL(authURL, "AUTH_SERVICE_URL"),
		ChatURL:       mustURL(chatURL, "CHAT_SERVICE_URL"),
		AgentURL:      mustURL(agentURL, "AGENT_SERVICE_URL"),
		BillingURL:    mustURL(billingURL, "BILLING_SERVICE_URL"),
		InternalToken: internalToken,
		CookieSecure:  cookieSecure,
		SessionTTL:    sessionTTL,
		TrustProxy:    trustProxy,
		LoginLimit:    gateway.Limit{Name: "login", Max: int64(loginPerMin), Window: time.Minute},
		APILimit:      gateway.Limit{Name: "api", Max: int64(apiPerMin), Window: time.Minute},
		AgentLimit:    gateway.Limit{Name: "agent", Max: int64(agentPerMin), Window: time.Minute},
	}, &gateway.Sessions{Redis: rdb, TTL: sessionTTL}, &gateway.RateLimiter{Redis: rdb})

	server.Run(addr, g, func(context.Context) { _ = rdb.Close() })
}
