package domain

import (
	"errors"
	"strings"
)

// TrialEmailKey is the identity a person's one trial is counted against: the address in lowercase,
// without a "+tag" in the local part, and for Gmail (gmail.com and googlemail.com, one mailbox)
// without dots and under gmail.com. Dots matter at every other provider, so they stay.
func TrialEmailKey(email string) (string, error) {
	address := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(address, "@")
	if at <= 0 || at == len(address)-1 || strings.Count(address, "@") != 1 || strings.ContainsAny(address, " \t\r\n") {
		return "", errors.New("not an email address")
	}
	local, host := address[:at], address[at+1:]
	local, _, _ = strings.Cut(local, "+")
	if host == "googlemail.com" {
		host = "gmail.com"
	}
	if host == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	if local == "" {
		return "", errors.New("not an email address")
	}
	return local + "@" + host, nil
}
