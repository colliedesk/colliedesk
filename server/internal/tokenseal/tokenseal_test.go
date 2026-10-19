package tokenseal_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

func key(fill byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)) }

func TestSealOpensOnlyWithTheSameContext(t *testing.T) {
	keys, err := tokenseal.ParseKeys("k1:" + key(1))
	if err != nil {
		t.Fatal(err)
	}
	keyID, sealed, err := keys.Seal([]byte("refresh-token"), "workspace-a")
	if err != nil || keyID != "k1" {
		t.Fatalf("seal: %q %v", keyID, err)
	}
	if bytes.Contains(sealed, []byte("refresh-token")) {
		t.Fatal("sealed bytes contain the plaintext")
	}
	_, again, _ := keys.Seal([]byte("refresh-token"), "workspace-a")
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals of the same token are identical: the nonce is not random")
	}
	opened, err := keys.Open(keyID, sealed, "workspace-a")
	if err != nil || string(opened) != "refresh-token" {
		t.Fatalf("open: %q %v", opened, err)
	}
	if _, err := keys.Open(keyID, sealed, "workspace-b"); err == nil {
		t.Fatal("a token sealed for workspace A opened for workspace B")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := keys.Open(keyID, sealed, "workspace-a"); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
}

func TestRotationSealsWithTheFirstKeyAndOpensWithAny(t *testing.T) {
	old, _ := tokenseal.ParseKeys("k1:" + key(1))
	_, sealed, _ := old.Seal([]byte("secret"), "w")
	rotated, err := tokenseal.ParseKeys("k2:" + key(2) + ", k1:" + key(1))
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := rotated.Open("k1", sealed, "w"); err != nil || string(opened) != "secret" {
		t.Fatalf("old key no longer opens: %v", err)
	}
	if rotated.Current() != "k2" || (tokenseal.Keys{}).Current() != "" {
		t.Fatalf("current key = %q", rotated.Current())
	}
	if id, _, _ := rotated.Seal([]byte("secret"), "w"); id != "k2" {
		t.Fatalf("sealed with %q, want the first key k2", id)
	}
	dropped, _ := tokenseal.ParseKeys("k2:" + key(2))
	if _, err := dropped.Open("k1", sealed, "w"); err == nil {
		t.Fatal("opened with a key id that is no longer configured")
	}
}

func TestParseKeysRefusesBadInput(t *testing.T) {
	if keys, err := tokenseal.ParseKeys(""); err != nil || keys.Configured() {
		t.Fatalf("empty: %v configured=%v", err, keys.Configured())
	}
	for name, value := range map[string]string{
		"no id":        key(1),
		"short key":    "k1:" + base64.StdEncoding.EncodeToString([]byte("short")),
		"not base64":   "k1:%%%",
		"duplicate id": "k1:" + key(1) + ",k1:" + key(2),
		"bad id":       "k 1:" + key(1),
	} {
		_, err := tokenseal.ParseKeys(value)
		if err == nil {
			t.Errorf("%s accepted", name)
			continue
		}
		if strings.Contains(err.Error(), key(1)) || strings.Contains(err.Error(), key(2)) {
			t.Errorf("%s: error repeats the key: %v", name, err)
		}
	}
}
