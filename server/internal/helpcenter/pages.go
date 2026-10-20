package helpcenter

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
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
	NoIndex, HeroSearch                            bool
	Query                                          string
	Crumbs                                         []link

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
}

func (s *Site) page(v view, title string, crumbs []link) page {
	siteTitle := v.title()
	full := siteTitle
	if title != "" {
		full = title + " · " + siteTitle
	}
	return page{Lang: v.site.Locale, Title: full, SiteTitle: siteTitle, Home: v.home, SearchURL: v.base + "/search",
		Style: v.base + stylePath + s.pages.styleName, NoIndex: v.site.Trial, Crumbs: crumbs}
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
	var slugs []string
	for current := n; current != nil; current = current.parent {
		slugs = append([]string{url.PathEscape(current.Slug)}, slugs...)
	}
	return v.base + "/c/" + strings.Join(slugs, "/")
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
		links = append(links, articleLink{Title: article.Title, URL: v.base + articlePath(article.Number, article.Slug), Current: article.Number == current})
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

func (s *Site) loadTree(request *http.Request, v view) (tree, error) {
	collections, err := s.config.Store.HelpCenterCollections(request.Context(), v.site.WorkspaceID)
	if err != nil {
		return tree{}, err
	}
	return buildTree(collections), nil
}

func (s *Site) home(response http.ResponseWriter, request *http.Request, v view) {
	if fresh(response, request, v.etag) {
		return
	}
	t, err := s.loadTree(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	roots := visible(t.roots)
	byCollection := map[string][]articleLink{}
	if len(roots) > 0 {
		ids := make([]string, len(roots))
		for index, root := range roots {
			ids[index] = root.ID
		}
		articles, err := s.config.Store.HelpCenterArticles(request.Context(), v.site.WorkspaceID, ids, 0, homeArticles)
		if err != nil {
			s.writeError(response, err)
			return
		}
		for _, article := range articles {
			byCollection[article.CollectionID] = append(byCollection[article.CollectionID], v.articleLinks([]domain.PublicKBArticle{article}, 0)...)
		}
	}
	data := s.page(v, "", nil)
	data.HeroSearch = true
	for _, root := range roots {
		data.Collections = append(data.Collections, v.card(root, byCollection[root.ID]))
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("home", data))
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
	if fresh(response, request, v.etag) {
		return
	}
	t, err := s.loadTree(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	slugs := strings.Split(strings.Trim(path, "/"), "/")
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
	for _, child := range visible(current.children) {
		data.Children = append(data.Children, v.card(child, nil))
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
	if fresh(response, request, v.etag) {
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
	// The number finds the article; any other slug, an old one included, moves to the current address.
	if match[2] != article.Slug {
		response.Header().Del("ETag")
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, v.base+articlePath(article.Number, article.Slug), http.StatusMovedPermanently)
		return
	}
	t, err := s.loadTree(request, v)
	if err != nil {
		s.writeError(response, err)
		return
	}
	neighbours, err := s.config.Store.HelpCenterArticles(request.Context(), v.site.WorkspaceID, []string{article.CollectionID}, 0, articleNeighbours)
	if err != nil {
		s.writeError(response, err)
		return
	}
	body, err := renderArticle(article.BodyMD, v.base)
	if err != nil {
		s.writeError(response, err)
		return
	}
	collection := t.byID[article.CollectionID]
	data := s.page(v, article.Title, v.crumbs(collection))
	data.Heading, data.Body = article.Title, body
	updated := article.UpdatedAt
	data.Updated, data.UpdatedISO = updated.Format("2 Jan 2006"), updated.Format(time.RFC3339)
	if collection != nil {
		data.CollectionName, data.Collection = collection.Name, v.collectionURL(collection)
	}
	data.Neighbours = v.articleLinks(neighbours, article.Number)
	s.writePage(response, request, v, http.StatusOK, s.pages.render("article", data))
}

func (s *Site) search(response http.ResponseWriter, request *http.Request, v view) {
	query := strings.Join(strings.Fields(request.URL.Query().Get("q")), " ")
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
	data.Query, data.Searched, data.HeroSearch = query, query != "", true
	if query != "" {
		articles, err := s.config.Store.SearchHelpCenter(request.Context(), v.site.WorkspaceID, query, searchResults)
		if err != nil {
			s.writeError(response, err)
			return
		}
		var t tree
		if len(articles) > 0 {
			if t, err = s.loadTree(request, v); err != nil {
				s.writeError(response, err)
				return
			}
		}
		for _, article := range articles {
			result := searchResult{Title: article.Title, URL: v.base + articlePath(article.Number, article.Slug), Excerpt: excerpt(article.Excerpt, excerptChars)}
			if collection := t.byID[article.CollectionID]; collection != nil {
				result.Collection = collection.Name
			}
			data.Results = append(data.Results, result)
		}
	}
	s.writePage(response, request, v, http.StatusOK, s.pages.render("search", data))
}
