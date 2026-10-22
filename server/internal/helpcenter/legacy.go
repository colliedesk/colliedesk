package helpcenter

import (
	"regexp"
	"strings"
	"unicode"
)

// Old Freshdesk help center addresses (/support/...) keep working on every address of a help center:
// an item the import brought over moves to its new page, anything else to the search or the home
// page, never to a bare 404. docs/internals/help-center.md, "Old Freshdesk addresses".

// LegacyKind is what an old Freshdesk help center path points at.
type LegacyKind int

const (
	// LegacyOther is any other page of the old portal (sign-in, tickets, a malformed item): home, 302.
	LegacyOther LegacyKind = iota
	// LegacyHome is /support, /support/home and /support/solutions: home, 301.
	LegacyHome
	// LegacyCategory, LegacyFolder and LegacyArticle name a Freshdesk knowledge base item by id.
	LegacyCategory
	LegacyFolder
	LegacyArticle
	// LegacySearch is the old portal's search; its term goes to the help center search.
	LegacySearch
)

// LegacyAddress is an old Freshdesk help center path, read.
type LegacyAddress struct {
	Kind LegacyKind
	// ID is the Freshdesk id of the category, folder or article.
	ID string
	// Words are the words of the item's slug ("7201-reset-password" gives "reset password"): the
	// search a visitor gets when the id leads to no public page.
	Words string
}

// Entity is the import ledger entity the address names an item of, or "" for the other kinds.
func (a LegacyAddress) Entity() string {
	switch a.Kind {
	case LegacyCategory:
		return "kb_category"
	case LegacyFolder:
		return "kb_folder"
	case LegacyArticle:
		return "kb_article"
	}
	return ""
}

// legacyLanguage is the language prefix of a multilingual Freshdesk portal (/de/, /pt-BR/).
var legacyLanguage = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$`)

// legacyItem is <id> or <id>-<slug>; Freshdesk ids never start with 0.
var legacyItem = regexp.MustCompile(`^([1-9][0-9]{0,18})(?:-(.*))?$`)

// ParseLegacyPath reads a request path (decoded, below the help center's address) as an old Freshdesk
// help center address. It reports false for any path outside /support (after an optional language).
func ParseLegacyPath(path string) (LegacyAddress, bool) {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) > 1 && segments[1] == "support" && legacyLanguage.MatchString(segments[0]) {
		segments = segments[1:]
	}
	if segments[0] != "support" {
		return LegacyAddress{}, false
	}
	rest := segments[1:]
	if len(rest) > 0 && rest[len(rest)-1] == "" {
		rest = rest[:len(rest)-1]
	}
	switch {
	case len(rest) == 0, len(rest) == 1 && (rest[0] == "home" || rest[0] == "solutions"):
		return LegacyAddress{Kind: LegacyHome}, true
	case rest[0] == "search":
		return LegacyAddress{Kind: LegacySearch}, true
	case rest[0] != "solutions":
	case len(rest) == 2:
		return legacyItemAddress(LegacyCategory, rest[1]), true
	case len(rest) == 3 && rest[1] == "folders":
		return legacyItemAddress(LegacyFolder, rest[2]), true
	case len(rest) == 3 && rest[1] == "articles":
		return legacyItemAddress(LegacyArticle, rest[2]), true
	}
	return LegacyAddress{Kind: LegacyOther}, true
}

func legacyItemAddress(kind LegacyKind, segment string) LegacyAddress {
	match := legacyItem.FindStringSubmatch(segment)
	if match == nil {
		return LegacyAddress{Kind: LegacyOther}
	}
	return LegacyAddress{Kind: kind, ID: match[1], Words: legacyWords(match[2])}
}

// legacyWords turns a slug into search words: letters and digits only, at most maxSearchChars.
func legacyWords(slug string) string {
	words := strings.Join(strings.FieldsFunc(slug, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
	if runes := []rune(words); len(runes) > maxSearchChars {
		words = strings.TrimSpace(string(runes[:maxSearchChars]))
	}
	return words
}
