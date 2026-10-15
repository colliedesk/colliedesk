package domain

import (
	"errors"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ErrInvalidEmail means the address cannot be turned into a trial key.
var ErrInvalidEmail = errors.New("not an email address")

// TrialEmailKey is the identity a person's one trial is counted against: the address in lowercase,
// without a "+tag" in the local part, and for Gmail (gmail.com and googlemail.com, one mailbox)
// without dots and under gmail.com. Dots matter at every other provider, so they stay. The address
// is NFC-normalized and a single trailing dot of the host is dropped, so equal spellings are one key.
// A "+" is treated as a tag separator everywhere; the rare provider where it is literal merges two
// people, which is accepted.
func TrialEmailKey(email string) (string, error) {
	address := norm.NFC.String(strings.ToLower(strings.TrimSpace(email)))
	at := strings.LastIndex(address, "@")
	if at <= 0 || at == len(address)-1 || strings.Count(address, "@") != 1 || strings.ContainsAny(address, " \t\r\n") {
		return "", ErrInvalidEmail
	}
	local, host := address[:at], strings.TrimSuffix(address[at+1:], ".")
	if host == "" || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return "", ErrInvalidEmail
	}
	local, _, _ = strings.Cut(local, "+")
	if host == "googlemail.com" {
		host = "gmail.com"
	}
	if host == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	if local == "" {
		return "", ErrInvalidEmail
	}
	return local + "@" + host, nil
}
