package cloudauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const clockLeeway = 60 * time.Second
const failedRefreshCooldown = 30 * time.Second

type Verifier struct {
	config    Config
	client    *http.Client
	cacheMu   sync.RWMutex
	refreshMu sync.Mutex
	keys      map[string]*rsa.PublicKey
	lastMiss  time.Time
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

func New(config Config) *Verifier {
	return &Verifier{config: config, client: &http.Client{Timeout: 5 * time.Second}, keys: map[string]*rsa.PublicKey{}}
}

// Verify checks a session token of the identity provider and returns its verified email. The token's
// authorized party (azp, the origin of the page that got it) must be one of the configured ones or
// one of parties: the workspace host the token is exchanged on.
func (verifier *Verifier) Verify(ctx context.Context, token string, parties ...string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("JWT must have three parts")
	}
	var header tokenHeader
	if err := decodePart(parts[0], &header); err != nil || header.Algorithm != "RS256" || header.KeyID == "" {
		return "", fmt.Errorf("JWT header is invalid")
	}
	key, err := verifier.key(ctx, header.KeyID)
	if err != nil {
		return "", err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode JWT signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return "", fmt.Errorf("verify JWT signature: %w", err)
	}
	var claims map[string]json.RawMessage
	if err := decodePart(parts[1], &claims); err != nil {
		return "", fmt.Errorf("decode JWT claims: %w", err)
	}
	return verifier.verifyClaims(claims, time.Now(), parties...)
}

func (verifier *Verifier) verifyClaims(claims map[string]json.RawMessage, now time.Time, parties ...string) (string, error) {
	var issuer, party, email string
	var expires, notBefore int64
	var verified bool
	if json.Unmarshal(claims["iss"], &issuer) != nil || issuer != verifier.config.Issuer {
		return "", fmt.Errorf("JWT issuer is invalid")
	}
	if json.Unmarshal(claims["exp"], &expires) != nil || now.After(time.Unix(expires, 0).Add(clockLeeway)) {
		return "", fmt.Errorf("JWT is expired")
	}
	rawNotBefore := claims["nbf"]
	if len(rawNotBefore) == 0 || string(rawNotBefore) == "null" || json.Unmarshal(rawNotBefore, &notBefore) != nil || now.Add(clockLeeway).Before(time.Unix(notBefore, 0)) {
		return "", fmt.Errorf("JWT is not active")
	}
	if json.Unmarshal(claims["azp"], &party) != nil || !(verifier.config.authorizedParties()[party] || party != "" && slices.Contains(parties, party)) {
		return "", fmt.Errorf("JWT authorized party is invalid")
	}
	if json.Unmarshal(claims[verifier.config.emailClaim()], &email) != nil || !strings.Contains(email, "@") {
		return "", fmt.Errorf("JWT email claim is invalid")
	}
	if json.Unmarshal(claims["email_verified"], &verified) != nil || !verified {
		return "", fmt.Errorf("JWT email is not verified")
	}
	return strings.ToLower(email), nil
}

func decodePart(part string, target any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, target)
}

func (verifier *Verifier) key(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	if key := verifier.cachedKey(keyID); key != nil {
		return key, nil
	}
	verifier.refreshMu.Lock()
	defer verifier.refreshMu.Unlock()
	if key := verifier.cachedKey(keyID); key != nil {
		return key, nil
	}
	verifier.cacheMu.RLock()
	recentMiss := time.Since(verifier.lastMiss) < failedRefreshCooldown
	verifier.cacheMu.RUnlock()
	if recentMiss {
		return nil, fmt.Errorf("JWT key ID is unknown")
	}
	keys, err := verifier.fetchKeys(ctx)
	if err != nil {
		verifier.recordMiss()
		return nil, err
	}
	key := keys[keyID]
	verifier.cacheMu.Lock()
	verifier.keys = keys
	if key == nil {
		verifier.lastMiss = time.Now()
	}
	verifier.cacheMu.Unlock()
	if key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("JWT key ID is unknown")
}

func (verifier *Verifier) cachedKey(keyID string) *rsa.PublicKey {
	verifier.cacheMu.RLock()
	defer verifier.cacheMu.RUnlock()
	return verifier.keys[keyID]
}

func (verifier *Verifier) recordMiss() {
	verifier.cacheMu.Lock()
	verifier.lastMiss = time.Now()
	verifier.cacheMu.Unlock()
}

func (verifier *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, verifier.config.JWKSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create JWKS request: %w", err)
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch JWKS: status %d", response.StatusCode)
	}
	var set jwkSet
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, item := range set.Keys {
		if key, ok := rsaKey(item); ok {
			keys[item.KeyID] = key
		}
	}
	return keys, nil
}
