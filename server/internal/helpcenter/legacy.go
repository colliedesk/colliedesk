package helpcenter

import (
	"context"
	"net/http"
	"net/url"
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

// legacyLanguage is the language prefix of a multilingual Freshdesk portal (/de/, /pt-BR/), in any case.
var legacyLanguage = regexp.MustCompile(`^(?i:[a-z]{2,3}(?:-[a-z0-9]{2,8})?)$`)

// legacyItem is <id> or <id>-<slug>; Freshdesk ids never start with 0.
var legacyItem = regexp.MustCompile(`^([1-9][0-9]{0,18})(?:-(.*))?$`)

// ParseLegacyPath reads a request path (decoded, below the help center's address) as an old Freshdesk
// help center address. It reports false for any path outside /support (after an optional language).
// The fixed words of the path match in any case (/Support/Solutions/...); the slug keeps its own.
func ParseLegacyPath(path string) (LegacyAddress, bool) {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	is := func(index int, word string) bool {
		return index < len(segments) && strings.EqualFold(segments[index], word)
	}
	if is(1, "support") && legacyLanguage.MatchString(segments[0]) {
		segments = segments[1:]
	}
	if !is(0, "support") {
		return LegacyAddress{}, false
	}
	segments = segments[1:]
	if len(segments) > 0 && segments[len(segments)-1] == "" {
		segments = segments[:len(segments)-1]
	}
	switch {
	case len(segments) == 0, len(segments) == 1 && (is(0, "home") || is(0, "solutions")):
		return LegacyAddress{Kind: LegacyHome}, true
	case is(0, "search"):
		return LegacyAddress{Kind: LegacySearch}, true
	case !is(0, "solutions"):
	case len(segments) == 2:
		return legacyItemAddress(LegacyCategory, segments[1]), true
	case len(segments) == 3 && is(1, "folders"):
		return legacyItemAddress(LegacyFolder, segments[2]), true
	case len(segments) == 3 && is(1, "articles"):
		return legacyItemAddress(LegacyArticle, segments[2]), true
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

// legacy answers an old Freshdesk address of an open help center. Every target is built from the
// help center's own address (SiteURL), never from the request's host or path, so no old address can
// send a visitor anywhere else. A redirect is never cached: an item published later takes over.
func (s *Site) legacy(response http.ResponseWriter, request *http.Request, v view, old LegacyAddress) {
	target, status, err := s.legacyTarget(request.Context(), v, old, request.URL.Query().Get("term"))
	if err != nil {
		s.writeError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, target, status)
}

// legacyTarget is where an old address goes and with which status: 301 to a page that is public now,
// 302 to the search with the old address's words (or home without words) otherwise.
func (s *Site) legacyTarget(ctx context.Context, v view, old LegacyAddress, term string) (string, int, error) {
	switch old.Kind {
	case LegacyHome:
		return v.address, http.StatusMovedPermanently, nil
	case LegacySearch:
		return v.searchAddress(term), http.StatusFound, nil
	case LegacyOther:
		return v.address, http.StatusFound, nil
	}
	found, err := s.config.Store.HelpCenterLegacyTarget(ctx, v.site.WorkspaceID, old.Entity(), old.ID)
	if err != nil {
		return "", 0, err
	}
	switch {
	case found.Number > 0:
		return v.address + ArticlePath(found.Number, found.Slug), http.StatusMovedPermanently, nil
	case found.CollectionID != "":
		t, err := s.loadTree(ctx, v)
		if err != nil {
			return "", 0, err
		}
		if n := t.byID[found.CollectionID]; n != nil && n.total > 0 {
			return v.address + n.path(), http.StatusMovedPermanently, nil
		}
	}
	return v.searchAddress(old.Words), http.StatusFound, nil
}

// searchAddress is the help center's search for words, or its home page without any.
func (v view) searchAddress(words string) string {
	return v.address + SearchPath(words)
}

// SearchPath is the path, below a help center's address, of its search for words (cleaned and
// bounded as the search page reads them), or "" (the home page) without any words.
func SearchPath(words string) string {
	words = strings.Join(strings.Fields(strings.ToValidUTF8(words, "")), " ")
	if runes := []rune(words); len(runes) > maxSearchChars {
		words = string(runes[:maxSearchChars])
	}
	if words == "" {
		return ""
	}
	return "/search?q=" + url.QueryEscape(words)
}
