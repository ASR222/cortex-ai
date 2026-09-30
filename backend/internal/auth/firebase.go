package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// googleCertsURL publishes the public keys that sign Firebase ID tokens.
const googleCertsURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

// Identity is the verified subset of a Firebase ID token.
type Identity struct {
	UID      string
	Email    string
	Name     string
	Picture  string
	Provider string
}

// TokenVerifier verifies Firebase ID tokens.
type TokenVerifier interface {
	Verify(ctx context.Context, idToken string) (Identity, error)
}

// ErrInvalidToken is returned for any token that fails verification.
var ErrInvalidToken = errors.New("invalid id token")

// FirebaseVerifier checks Firebase ID tokens without the Admin SDK.
//
// Verifying a Firebase token only needs Google's public signing keys and the
// project id, so no service-account secret is stored anywhere. The checks
// follow Firebase's documented rules: RS256 signature by a current Google key,
// audience = project id, issuer = securetoken.google.com/<project id>, and
// valid exp/iat/auth_time.
type FirebaseVerifier struct {
	projectID string
	certsURL  string
	http      *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

// NewFirebaseVerifier returns a verifier for the given Firebase project.
func NewFirebaseVerifier(projectID string) *FirebaseVerifier {
	return &FirebaseVerifier{
		projectID: projectID,
		certsURL:  googleCertsURL,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

type firebaseClaims struct {
	jwt.RegisteredClaims
	Email    string `json:"email"`
	Name     string `json:"name"`
	Picture  string `json:"picture"`
	AuthTime int64  `json:"auth_time"`
	Firebase struct {
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

// Verify validates idToken and returns the identity it carries.
func (v *FirebaseVerifier) Verify(ctx context.Context, idToken string) (Identity, error) {
	claims := &firebaseClaims{}
	_, err := jwt.ParseWithClaims(idToken, claims,
		func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			return v.key(ctx, kid)
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.projectID),
		jwt.WithIssuer("https://securetoken.google.com/"+v.projectID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if claims.Subject == "" || len(claims.Subject) > 128 {
		return Identity{}, fmt.Errorf("%w: bad subject", ErrInvalidToken)
	}
	if claims.AuthTime == 0 || time.Unix(claims.AuthTime, 0).After(time.Now().Add(30*time.Second)) {
		return Identity{}, fmt.Errorf("%w: bad auth_time", ErrInvalidToken)
	}
	return Identity{
		UID:      claims.Subject,
		Email:    claims.Email,
		Name:     claims.Name,
		Picture:  claims.Picture,
		Provider: claims.Firebase.SignInProvider,
	}, nil
}

// key returns the public key for kid, refreshing the cached key set when it
// has expired or the kid is unknown (Google rotates keys regularly).
func (v *FirebaseVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	fresh := time.Now().Before(v.expires)
	v.mu.RUnlock()
	if ok && fresh {
		return k, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

var maxAgeRe = regexp.MustCompile(`max-age=(\d+)`)

func (v *FirebaseVerifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch google certs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch google certs: status %d", resp.StatusCode)
	}

	var raw map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode google certs: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(raw))
	for kid, certPEM := range raw {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			keys[kid] = pub
		}
	}
	if len(keys) == 0 {
		return errors.New("no usable google certs")
	}

	ttl := time.Hour
	if m := maxAgeRe.FindStringSubmatch(resp.Header.Get("Cache-Control")); m != nil {
		if secs, err := strconv.Atoi(m[1]); err == nil {
			ttl = time.Duration(secs) * time.Second
		}
	}

	v.mu.Lock()
	v.keys = keys
	v.expires = time.Now().Add(ttl)
	v.mu.Unlock()
	return nil
}
