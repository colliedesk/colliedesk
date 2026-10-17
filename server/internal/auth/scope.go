package auth

import (
	"errors"
	"strings"
	"time"
)

// Tiers of an agent token. Each tier includes everything below it. read, draft and send act with the
// agent role; admin acts with its creator's role, which only an admin has when creating it and which
// is checked again on every call (a demoted creator's admin token acts as an agent).
const (
	ScopeRead  = "read"
	ScopeDraft = "draft"
	ScopeSend  = "send"
	ScopeAdmin = "admin"
)

// agentTokenMaxAge is how long after its creation an agent token stops working, however often it is
// used. Every use slides the expiry by the session lifetime, but never past this limit.
const agentTokenMaxAge = 365 * 24 * time.Hour

var scopeRank = map[string]int{ScopeRead: 1, ScopeDraft: 2, ScopeSend: 3, ScopeAdmin: 4}

// ScopeNames lists the tiers from the lowest to the highest.
var ScopeNames = []string{ScopeRead, ScopeDraft, ScopeSend, ScopeAdmin}

func ValidScope(scope string) bool {
	return scopeRank[scope] > 0
}

// ScopeAllows reports whether a session of tier have may do what tier need allows. A full session
// (empty have) may do everything; an unknown need is refused so a typo never opens a route.
func ScopeAllows(have, need string) bool {
	if have == "" {
		return true
	}
	return scopeRank[need] > 0 && scopeRank[have] >= scopeRank[need]
}

// ErrAdminTokenNeedsAdmin refuses an admin token to a person who is not an admin now: a token never
// has more rights than the person it acts as.
var ErrAdminTokenNeedsAdmin = errors.New("only a workspace admin can create an admin token")

// tokenRoleCeiling is the role ceiling a token of scope is stored with.
func tokenRoleCeiling(scope string) string {
	if scope == ScopeAdmin {
		return "admin"
	}
	return "agent"
}

// InvalidTokenError is a token request that names no valid label or tier.
type InvalidTokenError struct{ Reason string }

func (e *InvalidTokenError) Error() string { return e.Reason }

const maxTokenLabelRunes = 80

// checkTokenRequest returns the trimmed label of a valid request.
func checkTokenRequest(label, scope string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" || len([]rune(label)) > maxTokenLabelRunes {
		return "", &InvalidTokenError{"A token needs a label of 1 to 80 characters."}
	}
	if !ValidScope(scope) {
		return "", &InvalidTokenError{"Scope must be one of: " + strings.Join(ScopeNames, ", ") + "."}
	}
	return label, nil
}
