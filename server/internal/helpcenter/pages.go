package helpcenter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"html/template"
	"log"
	"net/http"
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

var pageNames = []string{"home", "collection", "article", "search", "missing", "busy"}

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
}

type searchResult struct {
	Title, URL, Excerpt, Collection string
}

// page is what a template shows; each page fills its own part.
type page struct {
	Lang, Title, SiteTitle, Home, SearchURL, Style string
	// Canonical (and Description, which a collection also shows) describe the page to search engines
	// and link previews.
	Canonical string
	// Markdown is the address of an article's Markdown, for AI assistants.
	Markdown            string
	NoIndex, HeroSearch bool
	Query               string
	Crumbs              []link

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

	// The brand (theme.go): its style sheet, the light-only color scheme, the logo in place of the
	// title, the favicon, the website link in the masthead and the contact links in the footer.
	BrandStyle, Favicon string
	LightOnly           bool
	Logo                *pageImage
	Website, Contact    *link
	Phone               *phoneLink
}

func (s *Site) page(v view, title string, crumbs []link) page {
	siteTitle := v.title()
	full := siteTitle
	if title != "" {
		full = title + " · " + siteTitle
	}
	data := page{Lang: v.site.Locale, Title: full, SiteTitle: siteTitle, Home: v.home, SearchURL: v.base + "/search",
		Style: v.base + stylePath + s.pages.styleName, NoIndex: v.site.Trial, Crumbs: crumbs}
	v.brand.fill(&data, v)
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
	return v.base + n.path()
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
	return append([]link{{Name: "Home", URL: v.home}}, trail...)
}

func (v view) articleLinks(articles []domain.PublicKBArticle, current int64) []articleLink {
	links := make([]articleLink, 0, len(articles))
	for _, article := range articles {
		links = append(links, articleLink{Title: article.Title, URL: v.base + ArticlePath(article.Number, article.Slug), Current: article.Number == current})
	}
	return links
}

func (v view) card(n *node, articles []articleLink) collectionCard {
	card := collectionCard{Name: n.Name, Description: n.Description, URL: v.collectionURL(n), Count: n.total, Articles: articles}
	for _, child := range visible(n.children) {
		card.Children = append(card.Children, link{Name: child.Name, URL: v.collectionURL(child)})
	}
	card.More = n.total > len(articles)
	return card
}

func (s *Site) loadTree(ctx context.Context, v view) (tree, error) {
	collections, err := s.config.Store.HelpCenterCollections(ctx, v.site.WorkspaceID)
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
		if articles, err = s.config.Store.HelpCenterArticles(ctx, v.site.WorkspaceID, ids, 0, homeArticles); err != nil {
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
	for _, root := range visible(t.roots) {
		data.Collections = append(data.Collections, v.card(root, byCollection[root.ID]))
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
	if canonical := v.base + CollectionPath(slugs); request.URL.EscapedPath() != canonical || len(query) > 1 ||
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
	articles, err := s.config.Store.HelpCenterArticles(request.Context(), v.site.WorkspaceID, []string{current.ID}, (pageNumber-1)*collectionPageSize, collectionPageSize)
	if err != nil {
		s.writeError(response, err)
		return
	}
	crumbs := v.crumbs(current)
	data := s.page(v, current.Name, crumbs[:len(crumbs)-1])
	data.Heading, data.Description = current.Name, current.Description
	data.Canonical = v.address + strings.TrimPrefix(v.collectionURL(current), v.base)
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
		previews, err := s.config.Store.HelpCenterArticles(request.Context(), v.site.WorkspaceID, ids, 0, homeArticles)
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
	article, err := s.config.Store.HelpCenterArticle(request.Context(), v.site.WorkspaceID, number)
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
		moveTo(response, request, v.base+ArticlePath(article.Number, article.Slug)+suffix)
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
	neighbours, err := s.config.Store.HelpCenterArticles(ctx, v.site.WorkspaceID, []string{article.CollectionID}, 0, articleNeighbours)
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
	data.Description, data.Canonical = rendered.description, v.address+ArticlePath(article.Number, article.Slug)
	data.Markdown = data.Canonical + markdownSuffix
	updated := article.UpdatedAt
	data.Updated, data.UpdatedISO = updated.Format("2 Jan 2006"), updated.Format(time.RFC3339)
	if collection != nil {
		data.CollectionName, data.Collection = collection.Name, v.collectionURL(collection)
	}
	data.Neighbours = v.articleLinks(neighbours, article.Number)
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
	title := "Search"
	if query != "" {
		title = "Search: " + query
	}
	data := s.page(v, title, []link{{Name: "Home", URL: v.home}})
	// Result pages are for visitors, not for search engines.
	data.Query, data.Searched, data.HeroSearch, data.NoIndex = query, query != "", true, true
	if query != "" {
		articles, err := s.config.Store.SearchHelpCenter(request.Context(), v.site.WorkspaceID, query, searchResults)
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
			result := searchResult{Title: article.Title, URL: v.base + ArticlePath(article.Number, article.Slug), Excerpt: excerpt(article.Excerpt, excerptChars)}
			if collection := t.byID[article.CollectionID]; collection != nil {
				result.Collection = collection.Name
			}
			data.Results = append(data.Results, result)
		}
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("search", data))
}
