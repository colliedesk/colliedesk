// Package standardwebhooks checks Standard Webhooks signatures, the scheme Polar uses under
// webhook-* headers and Svix (Resend) under svix-* headers.
package standardwebhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Tolerance is how far a webhook timestamp may be from now, in either direction.
const Tolerance = 5 * time.Minute

// ErrInvalidSignature covers every reason a webhook is rejected, so the caller cannot leak which
// check failed.
var ErrInvalidSignature = errors.New("invalid webhook signature")

// Verify checks an HMAC-SHA256 over "<id>.<timestamp>.<body>", sent base64 encoded as
// "v1,<signature>" (several, space separated, while a secret rotates). Secrets start with "whsec_".
// Newer secrets carry a base64 key after the prefix; older ones use the whole string as the key, so a
// signature made with either key is accepted.
func Verify(secret, id, timestamp, signatures string, body []byte, now time.Time) error {
	if secret == "" || id == "" || timestamp == "" || signatures == "" {
		return ErrInvalidSignature
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	if delta := now.Sub(time.Unix(seconds, 0)); delta > Tolerance || delta < -Tolerance {
		return ErrInvalidSignature
	}
	signed := []byte(id + "." + timestamp + ".")
	signed = append(signed, body...)
	for _, key := range signingKeys(secret) {
		mac := hmac.New(sha256.New, key)
		mac.Write(signed)
		expected := mac.Sum(nil)
		for _, candidate := range strings.Fields(signatures) {
			version, encoded, found := strings.Cut(candidate, ",")
			if !found || version != "v1" {
				continue
			}
			got, err := base64.StdEncoding.DecodeString(encoded)
			if err == nil && hmac.Equal(got, expected) {
				return nil
			}
		}
	}
	return ErrInvalidSignature
}

func signingKeys(secret string) [][]byte {
	keys := [][]byte{[]byte(secret)}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_")); err == nil && len(decoded) > 0 {
		keys = append(keys, decoded)
	}
	return keys
}
