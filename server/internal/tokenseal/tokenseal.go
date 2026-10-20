// Package tokenseal encrypts the long-lived provider tokens a workspace grants (a Gmail refresh token)
// before they reach the database. AES-256-GCM from the standard library, a random nonce per seal, and
// the caller's context (the workspace) as additional data, so a ciphertext copied into another
// workspace's row does not open. Keys carry an id, so a new key can be added in front of the old one.
package tokenseal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// KeySize is the length of every key: AES-256.
const KeySize = 32

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ErrUnknownKey means the sealed value names a key id that is no longer configured.
var ErrUnknownKey = errors.New("token key is not configured")

// Keys is an ordered key ring: the first key seals, every key opens.
type Keys struct {
	order []string
	aeads map[string]cipher.AEAD
}

// ParseKeys reads "id:base64key[,id:base64key...]". Empty means no keys. Errors never repeat key material.
func ParseKeys(value string) (Keys, error) {
	keys := Keys{aeads: map[string]cipher.AEAD{}}
	if strings.TrimSpace(value) == "" {
		return keys, nil
	}
	for index, entry := range strings.Split(value, ",") {
		id, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || !keyIDPattern.MatchString(id) {
			return Keys{}, fmt.Errorf("key %d must be written id:base64 with an id of letters, digits, - or _", index+1)
		}
		if _, seen := keys.aeads[id]; seen {
			return Keys{}, fmt.Errorf("key id %q is listed twice", id)
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != KeySize {
			return Keys{}, fmt.Errorf("key %q must be %d random bytes in standard base64 (openssl rand -base64 32)", id, KeySize)
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return Keys{}, fmt.Errorf("key %q: %w", id, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return Keys{}, fmt.Errorf("key %q: %w", id, err)
		}
		keys.order = append(keys.order, id)
		keys.aeads[id] = aead
	}
	return keys, nil
}

// Configured reports whether at least one key is set.
func (k Keys) Configured() bool { return len(k.order) > 0 }

// Current is the id of the key that seals, empty when none is set. A value sealed with another id should
// be sealed again, so the older key can be removed.
func (k Keys) Current() string {
	if !k.Configured() {
		return ""
	}
	return k.order[0]
}

// Seal encrypts plaintext bound to context with the first key and returns that key's id and nonce||ciphertext.
func (k Keys) Seal(plaintext []byte, context string) (string, []byte, error) {
	if !k.Configured() {
		return "", nil, ErrUnknownKey
	}
	id := k.order[0]
	aead := k.aeads[id]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, fmt.Errorf("token nonce: %w", err)
	}
	return id, aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}

// Open decrypts a value sealed with key id for the same context.
func (k Keys) Open(id string, sealed []byte, context string) ([]byte, error) {
	aead, ok := k.aeads[id]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("sealed token is too short")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(context))
	if err != nil {
		return nil, errors.New("sealed token does not open with its key")
	}
	return plaintext, nil
}
