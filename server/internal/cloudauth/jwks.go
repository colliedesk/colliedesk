package cloudauth

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"
)

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType string `json:"kty"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	KeyID   string `json:"kid"`
	N       string `json:"n"`
	E       string `json:"e"`
}

func rsaKey(item jwk) (*rsa.PublicKey, bool) {
	if item.KeyType != "RSA" || item.KeyID == "" || (item.Alg != "" && item.Alg != "RS256") || (item.Use != "" && item.Use != "sig") {
		return nil, false
	}
	modulus, nErr := base64.RawURLEncoding.DecodeString(item.N)
	exponent, eErr := base64.RawURLEncoding.DecodeString(item.E)
	if nErr != nil || eErr != nil || len(modulus) == 0 || len(exponent) == 0 || len(exponent) > 4 {
		return nil, false
	}
	e := 0
	for _, value := range exponent {
		e = e<<8 + int(value)
	}
	if e < 3 {
		return nil, false
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: e}, true
}
