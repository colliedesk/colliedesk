// Package githubsig checks the signature GitHub puts on a webhook delivery: X-Hub-Signature-256 is
// "sha256=" and the hex HMAC-SHA256 of the raw body with the App's webhook secret
// (https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries). It is not Standard
// Webhooks (standardwebhooks.Verify signs "id.timestamp.body" in base64): GitHub signs no id and no
// timestamp, so a replay is stopped by the delivery id the caller records.
package githubsig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// Header carries the signature.
const Header = "X-Hub-Signature-256"

// ErrInvalid is a missing, malformed or wrong signature, or no secret to check it with. The caller
// answers one 401 for all of them.
var ErrInvalid = errors.New("the webhook signature is missing or wrong")

// Sign is the header value GitHub sends for body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks the header value against the raw body, in constant time.
func Verify(secret, signature string, body []byte) error {
	if secret == "" {
		return ErrInvalid
	}
	given, ok := strings.CutPrefix(strings.TrimSpace(signature), "sha256=")
	if !ok {
		return ErrInvalid
	}
	sum, err := hex.DecodeString(given)
	if err != nil || len(sum) != sha256.Size {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return ErrInvalid
	}
	return nil
}
