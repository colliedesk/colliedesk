package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Authenticator codes (RFC 6238): HMAC-SHA1, 6 digits, a 30-second step, one step of drift either way.
// The step a code used is stored, so the same code never signs in twice.
const (
	totpDigits = 6
	totpPeriod = 30
	totpDrift  = 1
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret makes a 160-bit key, the size RFC 4226 recommends.
func newTOTPSecret() ([]byte, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("make authenticator key: %w", err)
	}
	return secret, nil
}

// TOTPKeyText is the key as people type it into an authenticator app: base32, no padding.
func TOTPKeyText(secret []byte) string { return totpEncoding.EncodeToString(secret) }

func totpStep(now time.Time) int64 { return now.Unix() / totpPeriod }

// totpCode is the code of one step.
func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

// matchTOTP finds the step of code around now, after lastStep; ok is false for a wrong or used code.
func matchTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	current := totpStep(now)
	for step := current - totpDrift; step <= current+totpDrift; step++ {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// otpauthURL is what an authenticator app's QR code holds (the Key Uri Format of Google Authenticator).
func otpauthURL(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	// SHA1, 6 digits and 30 seconds are the defaults every app assumes; leaving them out keeps the QR code small.
	query := url.Values{"secret": {TOTPKeyText(secret)}, "issuer": {issuer}}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// TOTPCode is the code an authenticator shows for secret at a time, as the operator's app computes it.
func TOTPCode(secret []byte, at time.Time) string { return totpCode(secret, totpStep(at)) }

// ParseTOTPKey reads a key as TOTPKeyText writes it.
func ParseTOTPKey(text string) ([]byte, error) {
	return totpEncoding.DecodeString(strings.ToUpper(strings.ReplaceAll(text, " ", "")))
}
