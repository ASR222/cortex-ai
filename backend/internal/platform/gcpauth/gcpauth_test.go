package gcpauth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fakeJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return "h." + payload + ".s"
}

func TestMetadataIDTokensCachesPerAudience(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Google", r.Header.Get("Metadata-Flavor"))
		calls.Add(1)
		_, _ = w.Write([]byte(fakeJWT(time.Now().Add(time.Hour)) + r.URL.Query().Get("audience")))
	}))
	defer srv.Close()

	m := &MetadataIDTokens{URL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	a1, err := m.Token(ctx, "https://auth")
	require.NoError(t, err)
	a2, _ := m.Token(ctx, "https://auth")
	b, _ := m.Token(ctx, "https://chat")

	require.Equal(t, a1, a2)
	require.NotEqual(t, a1, b)
	require.EqualValues(t, 2, calls.Load())
}

func TestMetadataIDTokensRefreshesNearExpiry(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(fakeJWT(time.Now().Add(2 * time.Minute)))) // inside the 5 min margin
	}))
	defer srv.Close()

	m := &MetadataIDTokens{URL: srv.URL, HTTP: srv.Client()}
	_, _ = m.Token(context.Background(), "a")
	_, _ = m.Token(context.Background(), "a")
	require.EqualValues(t, 2, calls.Load())
}

func TestFromEnv(t *testing.T) {
	t.Setenv("SERVICE_AUTH", "")
	require.Nil(t, FromEnv())
	t.Setenv("SERVICE_AUTH", "google")
	require.NotNil(t, FromEnv())
}
