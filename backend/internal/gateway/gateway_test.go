package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"cortexai/internal/platform/httpx"
)

const token = "internal-secret"

// seen records what an upstream service received.
type seen struct {
	mu      sync.Mutex
	path    string
	headers http.Header
}

func upstream(t *testing.T, s *seen) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.path, s.headers = r.URL.Path, r.Header.Clone()
		s.mu.Unlock()
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u
}

func fakeAuth(t *testing.T) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(httpx.HeaderInternalToken) != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/login":
			var in struct{ IDToken string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.IDToken != "good" {
				httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"code": "invalid_token", "message": "bad"})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"_id": "user-1", "credits": 50}})
		case "/internal/users/user-1":
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"_id": "user-1", "credits": 42}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u
}

type env struct {
	gw    *Gateway
	chat  *seen
	agent *seen
	redis *miniredis.Miniredis
}

func newEnv(t *testing.T, apiMax int64) *env {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	e := &env{chat: &seen{}, agent: &seen{}, redis: mr}
	e.gw = New(Config{
		AuthURL:       fakeAuth(t),
		ChatURL:       upstream(t, e.chat),
		AgentURL:      upstream(t, e.agent),
		BillingURL:    upstream(t, &seen{}),
		InternalToken: token,
		SessionTTL:    time.Hour,
		LoginLimit:    Limit{Name: "login", Max: 5, Window: time.Minute},
		APILimit:      Limit{Name: "api", Max: apiMax, Window: time.Minute},
		AgentLimit:    Limit{Name: "agent", Max: 100, Window: time.Minute},
	}, &Sessions{Redis: rdb, TTL: time.Hour}, &RateLimiter{Redis: rdb})
	return e
}

func (e *env) do(method, path, body string, hdr map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.gw.ServeHTTP(rec, req)
	return rec
}

var xhr = map[string]string{"X-Requested-With": "XMLHttpRequest"}

func (e *env) login(t *testing.T) *http.Cookie {
	t.Helper()
	rec := e.do(http.MethodPost, "/api/auth/login", `{"idToken":"good"}`, xhr, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			require.True(t, c.HttpOnly)
			require.Equal(t, http.SameSiteLaxMode, c.SameSite)
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestProtectedRoutesRequireSession(t *testing.T) {
	e := newEnv(t, 100)
	for _, p := range []string{"/api/me", "/api/chat/conversations", "/api/files/x/y"} {
		rec := e.do(http.MethodGet, p, "", nil, nil)
		require.Equal(t, http.StatusUnauthorized, rec.Code, p)
	}
	rec := e.do(http.MethodGet, "/api/me", "", nil, &http.Cookie{Name: CookieName, Value: "forged"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginRejectsBadToken(t *testing.T) {
	e := newEnv(t, 100)
	rec := e.do(http.MethodPost, "/api/auth/login", `{"idToken":"bad"}`, xhr, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, rec.Result().Cookies())
}

func TestProxyReplacesSpoofedIdentityHeaders(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	rec := e.do(http.MethodGet, "/api/chat/conversations", "", map[string]string{
		httpx.HeaderUserID:        "victim",
		httpx.HeaderInternalToken: "guess",
	}, c)
	require.Equal(t, http.StatusOK, rec.Code)

	e.chat.mu.Lock()
	defer e.chat.mu.Unlock()
	require.Equal(t, "/conversations", e.chat.path)
	require.Equal(t, "user-1", e.chat.headers.Get(httpx.HeaderUserID))
	require.Equal(t, token, e.chat.headers.Get(httpx.HeaderInternalToken))
	require.Empty(t, e.chat.headers.Get("Cookie"), "session cookie must not reach services")
	require.NotEmpty(t, e.chat.headers.Get(httpx.HeaderRequestID))
}

func TestInternalRoutesAreNeverForwarded(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	for _, p := range []string{
		"/api/chat/internal/users/user-2/conversations/abc/messages",
		"/api/chat/INTERNAL/x",
		"/api/agent/internal/users/x/conversations/y",
	} {
		rec := e.do(http.MethodGet, p, "", nil, c)
		require.Equal(t, http.StatusNotFound, rec.Code, p)
	}
	require.Empty(t, e.chat.path)
}

func TestCSRFHeaderRequiredForWrites(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	rec := e.do(http.MethodPost, "/api/chat/conversations", "{}", nil, c)
	require.Equal(t, http.StatusForbidden, rec.Code)
	rec = e.do(http.MethodPost, "/api/chat/conversations", "{}", xhr, c)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestFilesRewrite(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	rec := e.do(http.MethodGet, "/api/files/user-1/conv/abc-report.pdf", "", nil, c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/files/user-1/conv/abc-report.pdf", e.agent.path)
}

func TestMeReturnsFreshUser(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	rec := e.do(http.MethodGet, "/api/me", "", nil, c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"credits":42`)
}

func TestLogoutRevokesSession(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	require.Equal(t, http.StatusOK, e.do(http.MethodPost, "/api/auth/logout", "", xhr, c).Code)
	require.Equal(t, http.StatusUnauthorized, e.do(http.MethodGet, "/api/me", "", nil, c).Code)
}

func TestRedisStoresOnlyHashedSessionIDs(t *testing.T) {
	e := newEnv(t, 100)
	c := e.login(t)
	for _, k := range e.redis.Keys() {
		require.NotContains(t, k, c.Value)
	}
}

func TestRateLimit(t *testing.T) {
	e := newEnv(t, 3)
	c := e.login(t)
	for range 3 {
		require.Equal(t, http.StatusOK, e.do(http.MethodGet, "/api/me", "", nil, c).Code)
	}
	rec := e.do(http.MethodGet, "/api/me", "", nil, c)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestRequestIDFromClientIsNotTrusted(t *testing.T) {
	e := newEnv(t, 100)
	rec := e.do(http.MethodGet, "/healthz", "", map[string]string{httpx.HeaderRequestID: "attacker"}, nil)
	require.NotEqual(t, "attacker", rec.Header().Get(httpx.HeaderRequestID))
	_, _ = io.Copy(io.Discard, rec.Body)
}
