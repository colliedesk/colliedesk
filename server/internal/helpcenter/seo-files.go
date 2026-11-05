package helpcenter

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The files a help center serves for search engines and AI assistants: robots.txt (on a host of its
// own), sitemap.xml, llms.txt and the Markdown of each article (<article address>.md). They are read
// from the help center's own rows only and name only the address it serves on (view.address); a
// trial's pages are noindex, so its files list nothing and carry noindex too.

// markdownSuffix ends the address of an article's Markdown.
const markdownSuffix = ".md"

// homeURL is the canonical address of the home page: the host's "/", or the path address itself; in
// another language, <address>/<locale>.
func (v view) homeURL() string {
	if v.lang != "" {
		return v.rootAddress
	}
	if v.base == "" {
		return v.address + "/"
	}
	return v.address
}

// moveTo answers 301 to location, a canonical address of the page asked for.
func moveTo(response http.ResponseWriter, request *http.Request, location string) {
	response.Header().Del("ETag")
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, location, http.StatusMovedPermanently)
}

// writeFile sends one of the files with the page's cache headers (fresh already ran).
func (s *Site) writeFile(response http.ResponseWriter, request *http.Request, v view, contentType string, body []byte) {
	header := response.Header()
	securityHeaders(header)
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	if v.site.Trial {
		header.Set("X-Robots-Tag", "noindex")
	}
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(body)
	}
}

func (s *Site) robots(response http.ResponseWriter, request *http.Request, v view) {
	body := "User-agent: *\nDisallow:\n\nSitemap: " + v.address + "/sitemap.xml\n"
	if v.site.Trial {
		body = "User-agent: *\nDisallow: /\n"
	}
	if fresh(response, request, v.etag) {
		return
	}
	s.writeFile(response, request, v, "text/plain; charset=utf-8", []byte(body))
}

// indexed is what sitemap.xml and llms.txt list: the visible collections, top down, and the published
// articles; nothing for a trial.
func (s *Site) indexed(request *http.Request, v view) (tree, []*node, []domain.PublicKBArticle, error) {
	if v.site.Trial {
		return tree{}, nil, nil, nil
	}
	t, err := s.loadTree(request.Context(), v)
	if err != nil {
		return tree{}, nil, nil, err
	}
	articles, err := s.config.Store.HelpCenterIndex(request.Context(), v.site.WorkspaceID)
	if err != nil {
		return tree{}, nil, nil, err
	}
	var collections []*node
	var walk func([]*node)
	walk = func(level []*node) {
		for _, n := range visible(level) {
			collections = append(collections, n)
			walk(n.children)
		}
	}
	walk(t.roots)
	return t, collections, articles, nil
}

func (s *Site) sitemap(response http.ResponseWriter, request *http.Request, v view) {
	if fresh(response, request, v.etag) {
		return
	}
	t, collections, articles, err := s.indexed(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	// With other languages, every page lists its versions (hreflang) as search engines read them in a
	// sitemap; a help center in one language keeps the plain list.
	languages := len(v.site.Languages) > 0 && !v.site.Trial
	var translations []domain.PublicKBTranslation
	if languages {
		if translations, err = s.config.Store.HelpCenterTranslationIndex(request.Context(), v.site.WorkspaceID); err != nil {
			s.writeError(response, err)
			return
		}
	}
	var out bytes.Buffer
	if languages {
		out.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	} else {
		out.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	}
	type version struct{ lang, location string }
	entry := func(location string, modified *time.Time, versions []version) {
		out.WriteString("<url><loc>")
		_ = xml.EscapeText(&out, []byte(location))
		out.WriteString("</loc>")
		if modified != nil {
			out.WriteString("<lastmod>" + modified.UTC().Format(time.RFC3339) + "</lastmod>")
		}
		if len(versions) > 1 {
			for _, each := range versions {
				out.WriteString(`<xhtml:link rel="alternate" hreflang="`)
				_ = xml.EscapeText(&out, []byte(each.lang))
				out.WriteString(`" href="`)
				_ = xml.EscapeText(&out, []byte(each.location))
				out.WriteString(`"/>`)
			}
			out.WriteString(`<xhtml:link rel="alternate" hreflang="x-default" href="`)
			_ = xml.EscapeText(&out, []byte(versions[0].location))
			out.WriteString(`"/>`)
		}
		out.WriteString("</url>\n")
	}
	// all writes one page in each of its languages, each listing the others.
	all := func(paths pagePaths, modified map[string]*time.Time) {
		var versions []version
		for _, lang := range append([]string{""}, v.site.Languages...) {
			if path, ok := paths[lang]; ok {
				other := v.in(lang)
				location := other.rootAddress + path
				if path == "" {
					location = other.homeURL()
				}
				versions = append(versions, version{other.locale(), location})
			}
		}
		for _, each := range versions {
			lang := ""
			if each.lang != v.site.Locale {
				lang = each.lang
			}
			entry(each.location, modified[lang], versions)
		}
	}
	if !v.site.Trial {
		homes := pagePaths{"": ""}
		if languages {
			for lang := range shownIn(t.roots) {
				homes[lang] = ""
			}
		}
		all(homes, nil)
	}
	for _, n := range collections {
		paths := pagePaths{"": n.path()}
		if languages {
			for lang := range shownIn([]*node{n}) {
				paths[lang] = n.path()
			}
		}
		all(paths, nil)
	}
	byNumber := map[int64][]domain.PublicKBTranslation{}
	for _, translation := range translations {
		byNumber[translation.Number] = append(byNumber[translation.Number], translation)
	}
	for _, article := range articles {
		paths := pagePaths{"": ArticlePath(article.Number, article.Slug)}
		modified := map[string]*time.Time{"": &article.UpdatedAt}
		for _, translation := range byNumber[article.Number] {
			paths[translation.Locale] = ArticlePath(article.Number, translation.Slug)
			modified[translation.Locale] = &translation.UpdatedAt
		}
		all(paths, modified)
	}
	out.WriteString("</urlset>\n")
	s.writeFile(response, request, v, "application/xml; charset=utf-8", out.Bytes())
}

// markdownText escapes what would end a Markdown link text early.
var markdownText = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, "\n", " ", "\r", " ")

// llms is /llms.txt (llmstxt.org): the help center's title, then each collection that holds
// published articles with a link to the Markdown of each.
func (s *Site) llms(response http.ResponseWriter, request *http.Request, v view) {
	if fresh(response, request, v.etag) {
		return
	}
	_, collections, articles, err := s.indexed(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	var out strings.Builder
	out.WriteString("# " + markdownText.Replace(v.title()) + "\n")
	if !v.site.Trial {
		out.WriteString("\n> Answers from the help center of " + markdownText.Replace(v.site.Name) + ".\n")
	}
	byCollection := map[string][]domain.PublicKBArticle{}
	for _, article := range articles {
		byCollection[article.CollectionID] = append(byCollection[article.CollectionID], article)
	}
	section := func(name string, list []domain.PublicKBArticle) {
		if len(list) == 0 {
			return
		}
		out.WriteString("\n## " + markdownText.Replace(name) + "\n\n")
		for _, article := range list {
			out.WriteString("- [" + markdownText.Replace(article.Title) + "](" + v.address + ArticlePath(article.Number, article.Slug) + markdownSuffix + ")\n")
		}
	}
	for _, n := range collections {
		var names []string
		for current := n; current != nil; current = current.parent {
			names = append([]string{current.Name}, names...)
		}
		section(strings.Join(names, " / "), byCollection[n.ID])
		delete(byCollection, n.ID)
	}
	// Articles of a collection in the trash still answer by their number.
	var rest []domain.PublicKBArticle
	for _, article := range articles {
		if _, left := byCollection[article.CollectionID]; left {
			rest = append(rest, article)
		}
	}
	section("Other articles", rest)
	s.writeFile(response, request, v, "text/plain; charset=utf-8", []byte(out.String()))
}

// apiMediaPath matches the API path an article links its images and videos by.
var apiMediaPath = regexp.MustCompile(regexp.QuoteMeta(strings.TrimSuffix(domain.KBImagePath, "images/")) + `(images|media)/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// htmlComment is a note in the text that its page never shows.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// articleMarkdown is the article as written, under its title, with its images and videos on the help
// center's address and without the notes its page hides; the canonical address is its page.
func (s *Site) articleMarkdown(response http.ResponseWriter, request *http.Request, v view, article domain.PublicKBArticle) {
	text := htmlComment.ReplaceAllString(article.BodyMD, "")
	text = apiMediaPath.ReplaceAllString(text, v.address+"/$1/$2")
	body := "# " + strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(article.Title)) + "\n\n" + strings.TrimSpace(text) + "\n"
	response.Header().Set("Link", "<"+v.rootAddress+ArticlePath(article.Number, article.Slug)+`>; rel="canonical"`)
	s.writeFile(response, request, v, "text/markdown; charset=utf-8", []byte(body))
}
