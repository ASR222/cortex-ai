package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testProject = "cortex-test"

// fakeGoogle serves a self-signed certificate in the same format as Google's
// securetoken endpoint and signs tokens with the matching key.
type fakeGoogle struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	f := &fakeGoogle{key: key, kid: "kid-1"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]string{f.kid: certPEM})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) verifier() *FirebaseVerifier {
	v := NewFirebaseVerifier(testProject)
	v.certsURL = f.server.URL
	return v
}

func (f *fakeGoogle) token(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":       "https://securetoken.google.com/" + testProject,
		"aud":       testProject,
		"sub":       "firebase-uid-1",
		"iat":       now.Add(-time.Minute).Unix(),
		"exp":       now.Add(time.Hour).Unix(),
		"auth_time": now.Add(-time.Minute).Unix(),
		"email":     "ada@example.com",
		"name":      "Ada",
		"firebase":  map[string]any{"sign_in_provider": "google.com"},
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = f.kid
	s, err := tok.SignedString(f.key)
	require.NoError(t, err)
	return s
}

func TestFirebaseVerifier(t *testing.T) {
	g := newFakeGoogle(t)
	v := g.verifier()

	id, err := v.Verify(context.Background(), g.token(t, nil))
	require.NoError(t, err)
	require.Equal(t, "firebase-uid-1", id.UID)
	require.Equal(t, "ada@example.com", id.Email)
	require.Equal(t, "google.com", id.Provider)

	cases := map[string]func(jwt.MapClaims){
		"wrong audience": func(c jwt.MapClaims) { c["aud"] = "someone-else" },
		"wrong issuer":   func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" },
		"expired":        func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"empty subject":  func(c jwt.MapClaims) { c["sub"] = "" },
		"future auth":    func(c jwt.MapClaims) { c["auth_time"] = time.Now().Add(time.Hour).Unix() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), g.token(t, mutate))
			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}

func TestFirebaseVerifierRejectsForeignKey(t *testing.T) {
	g := newFakeGoogle(t)
	other := newFakeGoogle(t) // same kid, different key
	_, err := g.verifier().Verify(context.Background(), other.token(t, nil))
	require.True(t, errors.Is(err, ErrInvalidToken))
}

func TestFirebaseVerifierRejectsHS256(t *testing.T) {
	g := newFakeGoogle(t)
	// Classic "alg confusion" attack: sign with HMAC using public data.
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://securetoken.google.com/" + testProject, "aud": testProject, "sub": "x",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "auth_time": time.Now().Unix(),
	})
	tok.Header["kid"] = g.kid
	s, err := tok.SignedString([]byte("secret"))
	require.NoError(t, err)
	_, err = g.verifier().Verify(context.Background(), s)
	require.ErrorIs(t, err, ErrInvalidToken)
}
