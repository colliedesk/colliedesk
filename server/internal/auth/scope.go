package auth

import "strings"

// Tiers of an agent token. Each tier includes everything below it.
const (
	ScopeRead  = "read"
	ScopeDraft = "draft"
	ScopeSend  = "send"
)

var scopeRank = map[string]int{ScopeRead: 1, ScopeDraft: 2, ScopeSend: 3}

// ScopeNames lists the tiers from the lowest to the highest.
var ScopeNames = []string{ScopeRead, ScopeDraft, ScopeSend}

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
