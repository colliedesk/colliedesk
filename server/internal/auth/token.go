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

// credentialHash is the stored form of any bearer a client presents.
func credentialHash(token string) []byte {
	if !strings.HasPrefix(token, agentTokenPrefix) {
		return tokenHash(token)
	}
	hash := sha256.Sum256([]byte(agentTokenHashDomain + token))
	return hash[:]
}
