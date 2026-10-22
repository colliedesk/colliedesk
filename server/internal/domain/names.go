package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxHeaderNameRunes bounds a name that goes into a mail header.
const MaxHeaderNameRunes = 100

// A workspace name is 2 to 60 characters, at sign-up and on rename.
const (
	MinWorkspaceNameRunes = 2
	MaxWorkspaceNameRunes = 60
)

// unsafeNameRune reports a character that could break a header line or reorder the text around it:
// control characters and the text-direction overrides and isolates.
func unsafeNameRune(r rune) bool {
	return unicode.IsControl(r) || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩')
}

// NormalizeHeaderName trims raw and reports whether the result is a usable sender name: 1 to 100
// characters, no control characters and no text-direction overrides.
func NormalizeHeaderName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxHeaderNameRunes || strings.ContainsFunc(name, unsafeNameRune) {
		return "", false
	}
	return name, true
}

// NormalizeWorkspaceName is NormalizeHeaderName with the workspace name length: 2 to 60 characters.
func NormalizeWorkspaceName(raw string) (string, bool) {
	name, ok := NormalizeHeaderName(raw)
	if length := utf8.RuneCountInString(name); !ok || length < MinWorkspaceNameRunes || length > MaxWorkspaceNameRunes {
		return "", false
	}
	return name, true
}

// DefaultSenderName is the sender name a new email channel starts with: the workspace name with unsafe
// characters turned into spaces, cut to 100 characters, or the slug when nothing is left. The upgrade
// that introduced sender names gave existing channels theirs the same way.
func DefaultSenderName(workspaceName, slug string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unsafeNameRune(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(workspaceName, ""))
	cleaned = strings.TrimSpace(cleaned)
	if runes := []rune(cleaned); len(runes) > MaxHeaderNameRunes {
		cleaned = strings.TrimSpace(string(runes[:MaxHeaderNameRunes]))
	}
	if cleaned == "" {
		runes := []rune(slug)
		return string(runes[:min(len(runes), MaxHeaderNameRunes)])
	}
	return cleaned
}
