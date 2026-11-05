package helpcenter

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/text/language"
)

// The help center in several languages (docs/internals/help-center.md, "Languages"): the primary
// language keeps every address it had; a language customers see has the same pages below
// <base>/<locale>, showing only what is published in it.

// pagePaths are where's one page is in each of its languages, "" the primary one: the path below that
// language's root ("" for its home).
type pagePaths map[string]string

// languages fills the page's language menu and hreflang alternates, in the help center's order
// (primary first), by one rule on every page: the menu lists every language customers see, each
// linking this page in it where it exists and that language's home otherwise; hreflang lists only the
// versions that exist (paths), x-default the primary one. A help center with one language has neither.
func (v view) languages(data *page, paths pagePaths, alternates bool) {
	if len(v.site.Languages) == 0 {
		return
	}
	versions := 0
	for _, lang := range append([]string{""}, v.site.Languages...) {
		if _, ok := paths[lang]; ok {
			versions++
		}
	}
	for _, lang := range append([]string{""}, v.site.Languages...) {
		other := v.in(lang)
		path, ok := paths[lang]
		menu, absolute := other.root+path, other.rootAddress+path
		if !ok || path == "" {
			menu, absolute = other.home, other.homeURL()
			// The primary home says which language it is, or the browser's language would move the
			// reader back to the one they left (homeLanguage).
			if lang == "" {
				menu = strings.TrimSuffix(other.home, "/") + "/?" + url.Values{"lang": {v.site.Locale}}.Encode()
			}
		}
		data.Languages = append(data.Languages, languageLink{Lang: other.locale(), Name: languageName(other.locale()), URL: menu, Current: lang == v.lang})
		if alternates && ok && versions > 1 {
			data.Alternates = append(data.Alternates, languageLink{Lang: other.locale(), URL: absolute})
			if lang == "" {
				data.XDefault = absolute
			}
		}
	}
}

// splitLanguage reads the language a page path below the help center starts with: /fr/a/12-x is
// ("fr", "/a/12-x"). Only a language customers see counts, in any case; ok is false otherwise.
func (v view) splitLanguage(sub string) (lang, rest, segment string, ok bool) {
	trimmed := strings.TrimPrefix(sub, "/")
	segment, rest, _ = strings.Cut(trimmed, "/")
	if segment == "" {
		return "", sub, "", false
	}
	if rest != "" || strings.HasSuffix(trimmed, "/") {
		rest = "/" + rest
	}
	lang, ok = v.language(segment)
	return lang, rest, segment, ok
}

// languagePage tells whether a path below a language's root is one of the pages it has: home, search,
// a collection, an article (and its Markdown) or an old Freshdesk address. Images, videos, the style
// sheets and the files for crawlers exist once, below the help center itself.
func languagePage(rest string) bool {
	if rest == "" || rest == "/" || rest == "/search" || strings.HasPrefix(rest, "/c/") || strings.HasPrefix(rest, "/a/") {
		return true
	}
	_, legacy := ParseLegacyPath(rest)
	return legacy
}

// homeLanguage is the language the primary home moves a visitor to: the best match of their
// browser's languages among those customers see, "" for the primary one. A crawler sends no
// Accept-Language and stays. ?lang=<primary> (the language menu's link) keeps the primary home.
func (v view) homeLanguage(request *http.Request) string {
	header := request.Header.Get("Accept-Language")
	if header == "" || len(v.site.Languages) == 0 {
		return ""
	}
	desired, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(desired) == 0 {
		return ""
	}
	tags := []language.Tag{language.Make(v.site.Locale)}
	for _, lang := range v.site.Languages {
		tags = append(tags, language.Make(lang))
	}
	_, index, confidence := language.NewMatcher(tags).Match(desired...)
	if confidence < language.High || index == 0 {
		return ""
	}
	return v.site.Languages[index-1]
}
