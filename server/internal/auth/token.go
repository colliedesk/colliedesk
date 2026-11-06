package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// agentTokenPrefix starts every agent token. Secret scanners match it, and Authenticate uses it to
// pick how a bearer is hashed. A person's session token could start with it only with a probability
// of about 2^-60, and such a token would then simply not authenticate.
const agentTokenPrefix = "cdk_agent_"

// agentTokenHashDomain keeps the stored hash of an agent token apart from the plain SHA-256 that code
// older than the tier column uses for every bearer. That code looks the raw value up, finds no row
// and answers 401, so a rollback of the image never turns a scoped token into a full agent session.
const agentTokenHashDomain = "colliedesk-agent-token\x00"

func newToken() (string, []byte, error) {
	token, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	return token, tokenHash(token), nil
}

// newAgentToken returns a raw agent token and the hash to store for it.
func newAgentToken() (string, []byte, error) {
	random, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	token := agentTokenPrefix + random
	return token, credentialHash(token), nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// tokenHash is the stored form of a pairing link and of a person's session token.
func tokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

// The credentials of an OAuth grant: the one-time authorization code, the access token an AI app sends
// to /mcp and the refresh token it trades for the next pair. They share one hash domain of their own, so
// an older image (plain SHA-256 of every bearer) finds none of them, and a code or a refresh token sent as
// a bearer is refused by its prefix.
const (
	oauthCodePrefix    = "cdk_oac_"
	oauthAccessPrefix  = "cdk_oat_"
	oauthRefreshPrefix = "cdk_ort_"
	// oauthGrantPrefix names the random value whose hash fills sessions.token_hash of a grant; nobody
	// ever receives it.
	oauthGrantPrefix     = "cdk_oag_"
	oauthTokenHashDomain = "colliedesk-oauth\x00"
)

// credentialHash is the stored form of any bearer a client presents.
func credentialHash(token string) []byte {
	switch {
	case strings.HasPrefix(token, agentTokenPrefix):
		hash := sha256.Sum256([]byte(agentTokenHashDomain + token))
		return hash[:]
	case isOAuthCredential(token):
		hash := sha256.Sum256([]byte(oauthTokenHashDomain + token))
		return hash[:]
	}
	return tokenHash(token)
}

func isOAuthCredential(token string) bool {
	for _, prefix := range []string{oauthCodePrefix, oauthAccessPrefix, oauthRefreshPrefix, oauthGrantPrefix} {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

// newOAuthCredential returns a raw credential with prefix and the hash to store for it.
func newOAuthCredential(prefix string) (string, []byte, error) {
	random, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	token := prefix + random
	return token, credentialHash(token), nil
}
