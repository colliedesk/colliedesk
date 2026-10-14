package database

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

const scramIterations = 4096 // PostgreSQL's default scram_iterations

// scramVerifier builds the SCRAM-SHA-256 verifier PostgreSQL stores for a password (RFC 5802 and
// RFC 7677), in the form SCRAM-SHA-256$<iterations>:<salt>$<StoredKey>:<ServerKey>. Sending it as
// the role password keeps the plaintext out of statement text, server logs and statistics.
// PostgreSQL normalizes non-ASCII passwords with SASLprep before hashing; this code does not, so it
// accepts printable ASCII only (generated passwords are hex).
func scramVerifier(password string, salt []byte, iterations int) (string, error) {
	if password == "" {
		return "", errors.New("password is empty")
	}
	for _, r := range password {
		if r < 0x21 || r > 0x7e {
			return "", errors.New("password must be printable ASCII without spaces")
		}
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("derive SCRAM key: %w", err)
	}
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(salted, "Server Key")
	encode := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, encode(salt), encode(storedKey[:]), encode(serverKey)), nil
}

// newSCRAMVerifier uses a random 16-byte salt.
func newSCRAMVerifier(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate SCRAM salt: %w", err)
	}
	return scramVerifier(password, salt, scramIterations)
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}
