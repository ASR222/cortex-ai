package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"cortexai/internal/platform/gcpauth"
	"cortexai/internal/platform/httpx"
	"cortexai/internal/platform/internalauth"
)

// CookieName is the session cookie.
const CookieName = "cortex_session"

// Config configures the gateway.
type Config struct {
	AuthURL, ChatURL, AgentURL, BillingURL *url.URL
	InternalToken                          string
	CookieSecure                           bool
	SessionTTL                             time.Duration
	TrustProxy                             bool // read client IP from X-Forwarded-For (behind Caddy / Cloud Run)

	// IDTokens attaches Google identity tokens to upstream calls (Cloud Run).
	IDTokens gcpauth.TokenSource
	// StaticDir, if set, serves the built frontend from the gateway itself so
	// the site and API share one origin without a separate web server.
	StaticDir string

	LoginLimit Limit
	APILimit   Limit
	AgentLimit Limit
}

// Gateway is the public HTTP handler.
type Gateway struct {
	cfg      Config
	sessions *Sessions
	limiter  *RateLimiter
	auth     *internalauth.Client
	router   chi.Router
}

type ctxKey int

const userIDKey ctxKey = iota

func userIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok && id != ""
}

// New assembles the gateway routes.
func New(cfg Config, sessions *Sessions, limiter *RateLimiter) *Gateway {
	g := &Gateway{
		cfg:      cfg,
		sessions: sessions,
		limiter:  limiter,
		auth:     internalauth.NewClient(cfg.AuthURL.String(), cfg.InternalToken),
	}
	g.auth.IDTokens = cfg.IDTokens

	chatUp := NewUpstream(cfg.ChatURL, "/api/chat", "", cfg.InternalToken, false, cfg.IDTokens)
	agentUp := NewUpstream(cfg.AgentURL, "/api/agent", "", cfg.InternalToken, true, cfg.IDTokens)
	filesUp := NewUpstream(cfg.AgentURL, "/api/files", "/files", cfg.InternalToken, true, cfg.IDTokens)
	billingUp := NewUpstream(cfg.BillingURL, "/api/billing", "", cfg.InternalToken, false, cfg.IDTokens)

	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware(false), httpx.Logger, httpx.Recoverer, securityHeaders(cfg.CookieSecure))
	r.Get("/healthz", httpx.Healthz)

	r.Route("/api", func(r chi.Router) {
		r.Use(noStore)
		// Razorpay calls this directly; it is authenticated by its HMAC
		// signature in the billing service, not by a session or CSRF header.
		r.With(g.rateLimitByIP(cfg.LoginLimit), bodyLimit(256<<10)).
			Post("/billing/webhook", billingUp.ServeHTTP)

		r.Group(func(r chi.Router) {
			r.Use(csrfGuard, pathGuard)

			r.With(g.rateLimitByIP(cfg.LoginLimit), bodyLimit(16<<10)).
				Post("/auth/login", httpx.Handle(g.login))
			r.With(bodyLimit(1<<10)).Post("/auth/logout", httpx.Handle(g.logout))

			r.Group(func(r chi.Router) {
				r.Use(g.requireSession, g.rateLimitByUser(cfg.APILimit))
				r.Get("/me", httpx.Handle(g.me))
				r.With(bodyLimit(1<<20)).Handle("/chat/*", chatUp)
				r.With(bodyLimit(1<<20)).Handle("/billing/*", billingUp)
				r.Handle("/files/*", filesUp)
				r.With(g.rateLimitByUser(cfg.AgentLimit), bodyLimit(25<<20)).Handle("/agent/*", agentUp)
			})
		})
	})
	var spa http.Handler
	if cfg.StaticDir != "" {
		spa = spaHandler(cfg.StaticDir)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if spa != nil && !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api" {
			spa.ServeHTTP(w, r)
			return
		}
		httpx.WriteError(w, r, httpx.ErrNotFound)
	})
	g.router = r
	return g
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) { g.router.ServeHTTP(w, r) }

// --- handlers ---

type userResponse struct {
	User map[string]any `json:"user"`
}

func (g *Gateway) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDToken string `json:"idToken"`
	}
	if err := httpx.DecodeJSON(w, r, &in, 16<<10); err != nil {
		return err
	}
	var out userResponse
	if err := g.auth.Do(r.Context(), http.MethodPost, "/internal/login", in, &out); err != nil {
		return err
	}
	userID, _ := out.User["_id"].(string)
	if userID == "" {
		return errors.New("auth service returned a user without an id")
	}

	// Drop any session the browser already had, so a session id planted
	// before login can never become an authenticated one (session fixation).
	if c, err := r.Cookie(CookieName); err == nil {
		_ = g.sessions.Delete(r.Context(), c.Value)
	}
	token, err := g.sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, g.cookie(token, int(g.cfg.SessionTTL.Seconds())))
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (g *Gateway) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil {
		if err := g.sessions.Delete(r.Context(), c.Value); err != nil {
			return err
		}
	}
	http.SetCookie(w, g.cookie("", -1))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
	return nil
}

// me returns the current user fresh from the auth service, so credits shown
// in the UI are always the real balance rather than a cached snapshot.
func (g *Gateway) me(w http.ResponseWriter, r *http.Request) error {
	userID, _ := userIDFrom(r.Context())
	var out userResponse
	err := g.auth.Do(r.Context(), http.MethodGet, "/internal/users/"+url.PathEscape(userID), nil, &out)
	if internalauth.IsStatus(err, http.StatusNotFound) {
		http.SetCookie(w, g.cookie("", -1))
		return httpx.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (g *Gateway) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true, // not readable from JavaScript, so XSS cannot steal it
		Secure:   g.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// --- middleware ---

func (g *Gateway) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpx.WriteError(w, r, httpx.ErrUnauthorized)
			return
		}
		userID, err := g.sessions.Get(r.Context(), c.Value)
		if errors.Is(err, ErrNoSession) {
			http.SetCookie(w, g.cookie("", -1))
			httpx.WriteError(w, r, &httpx.Error{Status: http.StatusUnauthorized, Code: "session_expired", Message: "Your session has expired. Please sign in again."})
			return
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
	})
}

func (g *Gateway) rateLimit(lim Limit, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retry, err := g.limiter.Allow(r.Context(), lim, keyFn(r))
			if err != nil {
				// Fail open: a Redis hiccup should not take the whole API down.
				slog.WarnContext(r.Context(), "rate limiter unavailable", "err", err)
				next.ServeHTTP(w, r)
				return
			}
			if !ok {
				secs := int(math.Ceil(retry.Seconds()))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				httpx.WriteError(w, r, &httpx.Error{
					Status:  http.StatusTooManyRequests,
					Code:    "rate_limited",
					Title:   "Slow down",
					Message: fmt.Sprintf("Too many requests. Try again in %d seconds.", secs),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (g *Gateway) rateLimitByIP(lim Limit) func(http.Handler) http.Handler {
	return g.rateLimit(lim, g.clientIP)
}

func (g *Gateway) rateLimitByUser(lim Limit) func(http.Handler) http.Handler {
	return g.rateLimit(lim, func(r *http.Request) string {
		id, _ := userIDFrom(r.Context())
		return id
	})
}

// clientIP returns the caller's address. Behind Caddy the TCP peer is the
// proxy, so the real client is the last X-Forwarded-For entry (the one Caddy
// appended). Earlier entries are client-controlled and ignored.
func (g *Gateway) clientIP(r *http.Request) string {
	if g.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// csrfGuard requires a custom header on state-changing requests.
//
// Browsers only attach custom headers to cross-origin requests after a CORS
// preflight, which the gateway never approves, so a malicious site cannot
// forge requests that carry the user's cookie. SameSite=Lax on the cookie is
// the first layer; this is the second.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				httpx.WriteError(w, r, httpx.NewError(http.StatusForbidden, "csrf", "Missing X-Requested-With header."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func pathGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !safePath(r.URL.Path) || (r.URL.RawPath != "" && !safePath(r.URL.RawPath)) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				httpx.WriteError(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "Request is too large."))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(self)")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// noStore keeps API responses (user data) out of browser and proxy caches.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
