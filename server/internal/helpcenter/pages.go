package helpcenter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

//go:embed templates/*.html static/site.css
var files embed.FS

// pages are the parsed templates and the style sheet, named by its hash. fingerprint changes with
// any template or style, so a page's ETag does too even on a build without a version.
type pages struct {
	templates   map[string]*template.Template
	style       []byte
	styleName   string
	fingerprint string
}

var pageNames = []string{"home", "collection", "article", "search", "missing", "busy", "portal"}

func parsePages() pages {
	style, err := files.ReadFile("static/site.css")
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(style)
	parsed := pages{templates: map[string]*template.Template{}, style: style, styleName: "site-" + hex.EncodeToString(sum[:6]) + ".css"}
	all := sha256.New()
	for _, name := range append([]string{"layout"}, pageNames...) {
		source, err := files.ReadFile("templates/" + name + ".html")
		if err != nil {
			panic(err)
		}
		all.Write(source)
		if name != "layout" {
			parsed.templates[name] = template.Must(template.New("layout.html").ParseFS(files, "templates/layout.html", "templates/"+name+".html"))
		}
	}
	all.Write(style)
	parsed.fingerprint = hex.EncodeToString(all.Sum(nil)[:8])
	return parsed
}

func (p pages) render(name string, data page) []byte {
	var out bytes.Buffer
	if err := p.templates[name].Execute(&out, data); err != nil {
		log.Printf("help center: render %s: %v", name, err)
		return []byte("<!doctype html><title>Error</title><p>This page could not be shown.</p>")
	}
	return out.Bytes()
}

type link struct {
	Name, URL string
}

type articleLink struct {
	Title, URL string
	Current    bool
}

type collectionCard struct {
	Name, Description, URL string
	Count                  int
	Articles               []articleLink
	Children               []link
	More                   bool
	// The card's words in the page's language: how many articles, the link to all of them (or to the
	// collection) and the name of its list of sections.
	CountLabel, MoreLabel, SectionsLabel string
}

// languageLink is one language of the page in the language menu, or a hreflang alternate.
type languageLink struct {
	Lang, Name, URL string
	Current         bool
}

type searchResult struct {
	Title, URL, Excerpt, Collection string
}

// page is what a template shows; each page fills its own part.
type page struct {
	Lang, Title, SiteTitle, Home, SearchURL, Style string
	// T are the help center's own words in the page's language (ui-text.go).
	T uiText
	// Languages are the page in every language it exists in, for the menu; Alternates the same for
	// hreflang, with XDefault the primary language's address. Both are empty with one language.
	Languages, Alternates []languageLink
	XDefault              string
	// The page's sentences in its language: a collection's page counter, a search's result line, an
	// article's rail heading.
	PageLabel, ResultsLabel, RailLabel string
	// PrimaryHome and PrimaryName lead from a language with nothing published to the primary one.
	PrimaryHome, PrimaryName, PrimaryLang string
	// Canonical (and Description, which a collection also shows) describe the page to search engines
	// and link previews.
	Canonical string
	// Markdown is the address of an article's Markdown, for AI assistants.
	Markdown            string
	NoIndex, HeroSearch bool
	// GoogleVerification and BingVerification are the workspace's Search Console tokens.
	GoogleVerification, BingVerification string
	Query                                string
	Crumbs                               []link

	Collections []collectionCard

	Heading, Description string
	Children             []collectionCard
	Articles             []articleLink
	PageNumber, Pages    int
	Previous, Next       string

	Body                       template.HTML
	Updated, UpdatedISO        string
	CollectionName, Collection string
	Neighbours                 []articleLink

	Searched bool
	Results  []searchResult

	// StructuredData are the page's JSON-LD blocks (structured-data.go).
	StructuredData []template.JS

	// The brand (theme.go): its style sheet, the light-only color scheme, the logo in place of the
	// title, the favicon, the website link in the masthead and the contact links in the footer.
	BrandStyle, Favicon string
	LightOnly           bool
	Logo                *pageImage
	Website, Contact    *link
	Phone               *phoneLink

	// What a channel type adds (channels.HelpCenterAdditions): the header's link on every page, and on an
	// article its style sheet, its script and its box, with the article it is about.
	AskLink, AskLabel  string
	ExtraStyle, Script string
	ChatBox            bool
	BoxConfig          string
	// ChatText is the chat's words in the page's language (ChatWordsJSON), for the article's box.
	ChatText      string
	ArticleNumber int64
	ArticleTitle  string

	// The customer portal, on every page while it is on: the masthead link, and on its own pages what
	// that page shows.
	RequestsURL, RequestsLabel string
	Portal                     portalPage
}

func (s *Site) page(v view, title string, crumbs []link) page {
	siteTitle := v.title()
	full := siteTitle
	if title != "" {
		full = title + " · " + siteTitle
	}
	data := page{Lang: v.locale(), Title: full, SiteTitle: siteTitle, Home: v.home, SearchURL: v.root + "/search", T: v.text,
		Style: v.base + stylePath + s.pages.styleName, NoIndex: v.site.Trial, Crumbs: crumbs,
		GoogleVerification: v.site.SearchConsole.Google, BingVerification: v.site.SearchConsole.Bing}
	if v.site.Portal {
		data.RequestsURL, data.RequestsLabel = portalHome(v.base), portalLinkLabel(v.locale())
	}
	v.brand.fill(&data, v)
	if v.additions.Link != "" {
		data.AskLink, data.AskLabel = v.base+v.additions.Link, v.additions.LinkLabel
		if v.text.AskAI != "" {
			data.AskLabel = v.text.AskAI
		}
		// The chat answers in the page's language, from its translations (?lang).
		if v.lang != "" {
			separator := "?"
			if strings.Contains(data.AskLink, "?") {
				separator = "&"
			}
			data.AskLink += separator + url.Values{"lang": {v.lang}}.Encode()
		}
	}
	return data
}

// node is a collection in the tree; total counts the published articles in it and below it.
type node struct {
	domain.PublicKBCollection
	parent   *node
	children []*node
	total    int
}

type tree struct {
	byID  map[string]*node
	roots []*node
}

func buildTree(collections []domain.PublicKBCollection) tree {
	t := tree{byID: make(map[string]*node, len(collections))}
	for _, collection := range collections {
		t.byID[collection.ID] = &node{PublicKBCollection: collection}
	}
	for _, collection := range collections {
		current := t.byID[collection.ID]
		if parent, ok := t.byID[collection.ParentID]; ok {
			current.parent = parent
			parent.children = append(parent.children, current)
		} else {
			t.roots = append(t.roots, current)
		}
	}
	var count func(*node) int
	count = func(n *node) int {
		n.total = n.Published
		for _, child := range n.children {
			n.total += count(child)
		}
		return n.total
	}
	for _, root := range t.roots {
		count(root)
	}
	return t
}

// shownIn names the languages with a published article in or below the collections nodes ("" is
// the primary language).
func shownIn(nodes []*node) map[string]bool {
	shown := map[string]bool{}
	var walk func(*node)
	walk = func(n *node) {
		for lang, count := range n.PublishedIn {
			if count > 0 {
				shown[lang] = true
			}
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return shown
}

// visible keeps the collections with a published article in or below them; the others, and their
// names, stay private.
func visible(nodes []*node) []*node {
	shown := make([]*node, 0, len(nodes))
	for _, n := range nodes {
		if n.total > 0 {
			shown = append(shown, n)
		}
	}
	return shown
}

// find follows a path of slugs from the top; it returns nil unless every collection on it is visible.
func (t tree) find(slugs []string) *node {
	level := t.roots
	var found *node
	for _, slug := range slugs {
		found = nil
		for _, candidate := range visible(level) {
			if candidate.Slug == slug {
				found = candidate
				break
			}
		}
		if found == nil {
			return nil
		}
		level = found.children
	}
	return found
}

func (v view) collectionURL(n *node) string {
	return v.root + n.path()
}

// path is the collection's path below the help center's address.
func (n *node) path() string {
	var slugs []string
	for current := n; current != nil; current = current.parent {
		slugs = append([]string{current.Slug}, slugs...)
	}
	return CollectionPath(slugs)
}

// PublicCollectionPaths gives every collection with a published article in or below it its path
// below the help center's address (CollectionPath); the others have none, as on the pages.
func PublicCollectionPaths(collections []domain.PublicKBCollection) map[string]string {
	t := buildTree(collections)
	paths := make(map[string]string, len(t.byID))
	for id, n := range t.byID {
		if n.total > 0 {
			paths[id] = n.path()
		}
	}
	return paths
}

func (v view) crumbs(n *node) []link {
	var trail []link
	for current := n; current != nil; current = current.parent {
		trail = append([]link{{Name: current.Name, URL: v.collectionURL(current)}}, trail...)
	}
	return append([]link{{Name: v.text.Home, URL: v.home}}, trail...)
}

func (v view) articleLinks(articles []domain.PublicKBArticle, current int64) []articleLink {
	links := make([]articleLink, 0, len(articles))
	for _, article := range articles {
		links = append(links, articleLink{Title: article.Title, URL: v.root + ArticlePath(article.Number, article.Slug), Current: article.Number == current})
	}
	return links
}

func (v view) card(n *node, articles []articleLink) collectionCard {
	card := collectionCard{Name: n.Name, Description: n.Description, URL: v.collectionURL(n), Count: n.total, Articles: articles}
	for _, child := range visible(n.children) {
		card.Children = append(card.Children, link{Name: child.Name, URL: v.collectionURL(child)})
	}
	card.More = n.total > len(articles)
	card.CountLabel = v.count(n.total)
	card.MoreLabel = fmt.Sprintf(v.text.Open, n.Name)
	if card.More {
		card.MoreLabel = fmt.Sprintf(v.text.AllN, card.CountLabel)
	}
	card.SectionsLabel = fmt.Sprintf(v.text.SectionsOf, n.Name)
	return card
}

// count says how many articles in the page's language.
func (v view) count(n int) string {
	if n == 1 {
		return v.text.ArticleOne
	}
	return fmt.Sprintf(v.text.ArticlesN, n)
}

func (s *Site) loadTree(ctx context.Context, v view) (tree, error) {
	collections, err := s.config.Store.HelpCenterCollectionsIn(ctx, v.site.WorkspaceID, v.lang)
	if err != nil {
		return tree{}, err
	}
	return buildTree(collections), nil
}

func (s *Site) home(response http.ResponseWriter, request *http.Request, v view) {
	if fresh(response, request, v.etag) {
		return
	}
	data, err := s.homePage(request.Context(), v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("home", data))
}

// homePage is what the home page shows: the top collections with their first articles.
func (s *Site) homePage(ctx context.Context, v view) (page, error) {
	t, err := s.loadTree(ctx, v)
	if err != nil {
		return page{}, err
	}
	var articles []domain.PublicKBArticle
	if roots := visible(t.roots); len(roots) > 0 {
		ids := make([]string, len(roots))
		for index, root := range roots {
			ids[index] = root.ID
		}
		if articles, err = s.config.Store.HelpCenterArticlesIn(ctx, v.site.WorkspaceID, v.lang, ids, 0, homeArticles); err != nil {
			return page{}, err
		}
	}
	return s.homeOf(v, t, articles), nil
}

// homeOf is the home page of the tree t, with articles the first ones of its top collections.
func (s *Site) homeOf(v view, t tree, articles []domain.PublicKBArticle) page {
	byCollection := map[string][]articleLink{}
	for _, article := range articles {
		byCollection[article.CollectionID] = append(byCollection[article.CollectionID], v.articleLinks([]domain.PublicKBArticle{article}, 0)...)
	}
	data := s.page(v, "", nil)
	data.HeroSearch, data.Canonical = true, v.homeURL()
	data.StructuredData = []template.JS{v.breadcrumbs(nil, link{Name: v.text.Home, URL: v.home})}
	for _, root := range visible(t.roots) {
		data.Collections = append(data.Collections, v.card(root, byCollection[root.ID]))
	}
	// The home of every language with something published in it; the primary one always.
	paths := pagePaths{"": ""}
	for lang := range shownIn(t.roots) {
		paths[lang] = ""
	}
	v.languages(&data, paths, true)
	// A language shown before anything is published in it says so, and leads to the primary language.
	if v.lang != "" && len(data.Collections) == 0 {
		// Nothing of its own for search engines either (it is not in hreflang or the sitemap).
		data.NoIndex = true
		primary := v.in("")
		data.PrimaryHome = strings.TrimSuffix(primary.home, "/") + "/?" + url.Values{"lang": {v.site.Locale}}.Encode()
		data.PrimaryName, data.PrimaryLang = languageName(v.site.Locale), v.site.Locale
	}
	return data
}

func (s *Site) collection(response http.ResponseWriter, request *http.Request, v view, path string) {
	pageNumber := 1
	if raw := request.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > domain.MaxKBArticles {
			s.writeSiteNotFound(response, v)
			return
		}
		pageNumber = parsed
	}
	// One address per collection page: its slugs without empty parts, and no query but a page past the first.
	var slugs []string
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			slugs = append(slugs, part)
		}
	}
	query := request.URL.Query()
	if canonical := v.root + CollectionPath(slugs); request.URL.EscapedPath() != canonical || len(query) > 1 ||
		len(query) == 1 && !query.Has("page") || pageNumber == 1 && query.Has("page") {
		if pageNumber > 1 {
			canonical += "?page=" + strconv.Itoa(pageNumber)
		}
		moveTo(response, request, canonical)
		return
	}
	if fresh(response, request, v.etag) {
		return
	}
	t, err := s.loadTree(request.Context(), v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	current := t.find(slugs)
	if len(slugs) > domain.MaxKBDepth || current == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	pages := max(1, (current.Published+collectionPageSize-1)/collectionPageSize)
	if pageNumber > pages {
		s.writeSiteNotFound(response, v)
		return
	}
	articles, err := s.config.Store.HelpCenterArticlesIn(request.Context(), v.site.WorkspaceID, v.lang, []string{current.ID}, (pageNumber-1)*collectionPageSize, collectionPageSize)
	if err != nil {
		s.writeError(response, err)
		return
	}
	crumbs := v.crumbs(current)
	data := s.page(v, current.Name, crumbs[:len(crumbs)-1])
	data.StructuredData = []template.JS{v.breadcrumbs(crumbs[:len(crumbs)-1], crumbs[len(crumbs)-1])}
	data.Heading, data.Description = current.Name, current.Description
	data.Canonical = v.rootAddress + strings.TrimPrefix(v.collectionURL(current), v.root)
	paths := pagePaths{}
	for lang := range shownIn([]*node{current}) {
		paths[lang] = current.path()
	}
	v.languages(&data, paths, pageNumber == 1)
	if pageNumber > 1 {
		data.Canonical += "?page=" + strconv.Itoa(pageNumber)
	}
	// Each section lists its first articles, as the home page's topics do, so a collection that only
	// holds sections still reaches an article in one click.
	children := visible(current.children)
	byChild := map[string][]articleLink{}
	if len(children) > 0 {
		ids := make([]string, len(children))
		for index, child := range children {
			ids[index] = child.ID
		}
		previews, err := s.config.Store.HelpCenterArticlesIn(request.Context(), v.site.WorkspaceID, v.lang, ids, 0, homeArticles)
		if err != nil {
			s.writeError(response, err)
			return
		}
		for _, article := range previews {
			byChild[article.CollectionID] = append(byChild[article.CollectionID], v.articleLinks([]domain.PublicKBArticle{article}, 0)...)
		}
	}
	for _, child := range children {
		data.Children = append(data.Children, v.card(child, byChild[child.ID]))
	}
	data.Articles = v.articleLinks(articles, 0)
	data.PageNumber, data.Pages = pageNumber, pages
	data.PageLabel = fmt.Sprintf(v.text.PageOf, pageNumber, pages)
	here := v.collectionURL(current)
	if pageNumber > 1 {
		data.Previous = here
		if pageNumber > 2 {
			data.Previous += "?page=" + strconv.Itoa(pageNumber-1)
		}
	}
	if pageNumber < pages {
		data.Next = here + "?page=" + strconv.Itoa(pageNumber+1)
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("collection", data))
}

func (s *Site) article(response http.ResponseWriter, request *http.Request, v view, address string) {
	address, asMarkdown := strings.CutSuffix(address, markdownSuffix)
	suffix := ""
	if asMarkdown {
		suffix = markdownSuffix
	}
	match := articleAddress.FindStringSubmatch(address)
	if match == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	number, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		s.writeSiteNotFound(response, v)
		return
	}
	// A query names no other article: the address without it is the canonical one, checked below.
	if request.URL.RawQuery == "" && fresh(response, request, v.etag) {
		return
	}
	article, err := s.config.Store.HelpCenterArticleIn(request.Context(), v.site.WorkspaceID, number, v.lang)
	if errors.Is(err, domain.ErrKBArticleNotFound) && v.lang != "" {
		// Not published in this language (yet): the article in the primary language, at its own address
		// in one step. 302, since a translation may come; none in the primary language is a 404 here.
		primary, err := s.config.Store.HelpCenterArticle(request.Context(), v.site.WorkspaceID, number)
		if errors.Is(err, domain.ErrKBArticleNotFound) {
			s.writeSiteNotFound(response, v)
			return
		}
		if err != nil {
			s.writeError(response, err)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, v.base+ArticlePath(number, primary.Slug)+suffix, http.StatusFound)
		return
	}
	if errors.Is(err, domain.ErrKBArticleNotFound) {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	// The number finds the article; any other slug, an old one included, or a query moves to the
	// current address.
	if match[2] != article.Slug || request.URL.RawQuery != "" {
		moveTo(response, request, v.root+ArticlePath(article.Number, article.Slug)+suffix)
		return
	}
	if asMarkdown {
		s.articleMarkdown(response, request, v, article)
		return
	}
	data, plain, err := s.articlePage(request.Context(), v, article)
	if err != nil {
		s.writeError(response, err)
		return
	}
	if plain {
		// The next visit may get the rendered page under the same ETag.
		response.Header().Del("ETag")
		response.Header().Set("Cache-Control", "no-store")
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("article", data))
}

// articlePage is what an article's page shows. plain is true when it shows the text as written because
// rendering took too long.
func (s *Site) articlePage(ctx context.Context, v view, article domain.PublicKBArticle) (data page, plain bool, err error) {
	t, err := s.loadTree(ctx, v)
	if err != nil {
		return page{}, false, err
	}
	neighbours, err := s.config.Store.HelpCenterArticlesIn(ctx, v.site.WorkspaceID, v.lang, []string{article.CollectionID}, 0, articleNeighbours)
	if err != nil {
		return page{}, false, err
	}
	data, plain = s.articleOf(v, t, article, neighbours)
	return data, plain, nil
}

// articleOf is the page of article in the tree t, beside neighbours, the first articles of its
// collection. plain is true when it shows the text as written because rendering took too long.
func (s *Site) articleOf(v view, t tree, article domain.PublicKBArticle, neighbours []domain.PublicKBArticle) (data page, plain bool) {
	rendered := s.renders.article(v.site.WorkspaceID, article.Number, v.base, article.BodyMD, article.Videos...)
	collection := t.byID[article.CollectionID]
	data = s.page(v, article.Title, v.crumbs(collection))
	data.Heading, data.Body = article.Title, rendered.body
	data.Description, data.Canonical = rendered.description, v.rootAddress+ArticlePath(article.Number, article.Slug)
	data.Markdown = data.Canonical + markdownSuffix
	// The article's own search title and description replace the defaults.
	if article.SEOTitle != "" {
		data.Title = article.SEOTitle
	}
	if article.SEODescription != "" {
		data.Description = article.SEODescription
	}
	data.StructuredData = []template.JS{
		v.breadcrumbs(data.Crumbs, link{Name: article.Title, URL: v.root + ArticlePath(article.Number, article.Slug)}),
		v.articleData(article, data.Canonical, data.Description)}
	if len(rendered.faq) > 0 {
		data.StructuredData = append(data.StructuredData, faqData(rendered.faq))
	}
	updated := article.UpdatedAt
	data.Updated, data.UpdatedISO = updated.Format(v.text.DateFormat), updated.Format(time.RFC3339)
	data.RailLabel = v.text.RelatedArticles
	if collection != nil {
		data.CollectionName, data.Collection = collection.Name, v.collectionURL(collection)
		data.RailLabel = fmt.Sprintf(v.text.InCollection, collection.Name)
	}
	data.Neighbours = v.articleLinks(neighbours, article.Number)
	paths := pagePaths{}
	for lang, slug := range article.Alternates {
		paths[lang] = ArticlePath(article.Number, slug)
	}
	v.languages(&data, paths, true)
	if a := v.additions; a.ArticleBox && a.ArticleScript != "" {
		data.ExtraStyle, data.Script, data.ChatBox, data.BoxConfig = v.base+a.ArticleStyle, v.base+a.ArticleScript, true, a.BoxConfig
		data.ChatText = ChatWordsJSON(v.locale())
		data.ArticleNumber, data.ArticleTitle = article.Number, article.Title
	}
	return data, rendered.plain
}

func (s *Site) search(response http.ResponseWriter, request *http.Request, v view) {
	// Postgres refuses NUL and invalid UTF-8 in text; neither can be part of a word anyway.
	query := strings.ReplaceAll(strings.ToValidUTF8(request.URL.Query().Get("q"), ""), "\x00", "")
	query = strings.Join(strings.Fields(query), " ")
	if utf8.RuneCountInString(query) > maxSearchChars {
		query = string([]rune(query)[:maxSearchChars])
	}
	if fresh(response, request, v.etag) {
		return
	}
	title := v.text.Search
	if query != "" {
		title = fmt.Sprintf(v.text.SearchFor, query)
	}
	data := s.page(v, title, []link{{Name: v.text.Home, URL: v.home}})
	data.StructuredData = []template.JS{v.breadcrumbs(data.Crumbs, link{Name: v.text.Search, URL: v.root + "/search"})}
	// Result pages are for visitors, not for search engines.
	data.Query, data.Searched, data.HeroSearch, data.NoIndex = query, query != "", true, true
	search := "/search"
	if query != "" {
		search += "?" + url.Values{"q": {query}}.Encode()
	}
	paths := pagePaths{"": search}
	for _, lang := range v.site.Languages {
		paths[lang] = search
	}
	v.languages(&data, paths, false)
	if query != "" {
		articles, err := s.config.Store.SearchHelpCenterIn(request.Context(), v.site.WorkspaceID, v.lang, query, searchResults)
		if err != nil {
			s.writeError(response, err)
			return
		}
		var t tree
		if len(articles) > 0 {
			if t, err = s.loadTree(request.Context(), v); err != nil {
				s.writeError(response, err)
				return
			}
		}
		for _, article := range articles {
			result := searchResult{Title: article.Title, URL: v.root + ArticlePath(article.Number, article.Slug), Excerpt: excerpt(article.Excerpt, excerptChars)}
			if collection := t.byID[article.CollectionID]; collection != nil {
				result.Collection = collection.Name
			}
			data.Results = append(data.Results, result)
		}
		switch len(data.Results) {
		case 0:
			data.ResultsLabel = fmt.Sprintf(v.text.NoResults, query)
		case 1:
			data.ResultsLabel = fmt.Sprintf(v.text.ResultOne, 1, query)
		default:
			data.ResultsLabel = fmt.Sprintf(v.text.ResultsN, len(data.Results), query)
		}
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("search", data))
}
