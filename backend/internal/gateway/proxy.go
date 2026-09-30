package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"cortexai/internal/platform/gcpauth"
	"cortexai/internal/platform/httpx"
)

// headersFromClient lists headers a client must never be able to set on an
// upstream request. X-User-Id in particular is what services use to identify
// the caller, so it may only ever come from the gateway itself.
var headersFromClient = []string{
	httpx.HeaderUserID,
	httpx.HeaderInternalToken,
	httpx.HeaderRequestID,
	"Cookie",        // services have no use for the session cookie
	"Authorization", // only the gateway's own Google identity token may reach services
	"X-User-Email",
	"X-User-Avatar",
}

// Upstream forwards requests under a public prefix to an internal service.
type Upstream struct {
	proxy *httputil.ReverseProxy
}

// NewUpstream builds a reverse proxy to target. Requests to
// publicPrefix + "/rest" are sent to target + upstreamPrefix + "/rest".
// streaming disables response buffering so Server-Sent Events reach the
// browser token by token. idTokens (Cloud Run only) adds a Google identity
// token for the target so its IAM check accepts the gateway.
func NewUpstream(target *url.URL, publicPrefix, upstreamPrefix, internalToken string, streaming bool,
	idTokens gcpauth.TokenSource) *Upstream {
	audience := strings.TrimRight(target.String(), "/")
	p := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			rest := strings.TrimPrefix(pr.In.URL.Path, publicPrefix)
			pr.Out.URL.Path = strings.TrimRight(target.Path, "/") + upstreamPrefix + rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host

			for _, h := range headersFromClient {
				pr.Out.Header.Del(h)
			}
			pr.Out.Header.Set(httpx.HeaderInternalToken, internalToken)
			pr.Out.Header.Set(httpx.HeaderRequestID, httpx.RequestID(pr.In.Context()))
			if uid, ok := userIDFrom(pr.In.Context()); ok {
				pr.Out.Header.Set(httpx.HeaderUserID, uid)
			}
			if idTokens != nil {
				tok, err := idTokens.Token(pr.In.Context(), audience)
				if err != nil {
					// The upstream will answer 401/403, which the client sees.
					slog.ErrorContext(pr.In.Context(), "identity token", "audience", audience, "err", err)
				} else {
					pr.Out.Header.Set("Authorization", "Bearer "+tok)
				}
			}
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.ErrorContext(r.Context(), "upstream error", "target", target.Host, "err", err)
			httpx.WriteJSON(w, http.StatusBadGateway, &httpx.Error{
				Code: "upstream_unavailable", Message: "Service is temporarily unavailable. Please try again.",
			})
		},
	}
	if streaming {
		p.FlushInterval = -1
	} else {
		p.FlushInterval = 100 * time.Millisecond
	}
	return &Upstream{proxy: p}
}

func (u *Upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.proxy.ServeHTTP(w, r)
}

// safePath rejects paths that try to reach internal-only routes or climb out
// of the prefix. Internal services expose /internal/... endpoints for each
// other; the gateway must never forward those.
func safePath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		s := strings.ToLower(seg)
		if s == "internal" || s == ".." || s == "." || strings.Contains(s, "%") || strings.Contains(s, "\\") {
			return false
		}
	}
	return true
}
