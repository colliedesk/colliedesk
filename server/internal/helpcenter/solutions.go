package helpcenter

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// The header link of the page a reader is on (page.Tab): Home on the home page, Solutions on the
// Solutions page and on every collection and article, which are inside it. A search or a missing page
// is neither.
const (
	tabHome      = "home"
	tabSolutions = "solutions"
)

// solutions answers the Solutions page, at the collections' own root (CollectionPath of no slugs):
// every collection with a published article, in the order of the tree, each with its own first
// articles, as a Freshdesk portal's Solutions tab lists its categories and folders.
func (s *Site) solutions(response http.ResponseWriter, request *http.Request, v view, t tree) {
	var shown []*node
	var walk func([]*node)
	walk = func(level []*node) {
		for _, n := range visible(level) {
			shown = append(shown, n)
			walk(n.children)
		}
	}
	walk(t.roots)
	byCollection := map[string][]articleLink{}
	if len(shown) > 0 {
		ids := make([]string, len(shown))
		for index, n := range shown {
			ids[index] = n.ID
		}
		articles, err := s.config.Store.HelpCenterArticlesIn(request.Context(), v.site.WorkspaceID, v.site.HelpCenterID, v.lang, ids, 0, collectionPageSize)
		if err != nil {
			s.writeError(response, err)
			return
		}
		for _, article := range articles {
			byCollection[article.CollectionID] = append(byCollection[article.CollectionID], v.articleLinks([]domain.PublicKBArticle{article}, 0)...)
		}
	}
	home := link{Name: v.text.Home, URL: v.home}
	data := s.page(v, v.text.Solutions, []link{home})
	data.Heading, data.Tab = v.text.Solutions, tabSolutions
	data.Canonical = v.rootAddress + CollectionPath(nil)
	data.StructuredData = []template.JS{v.breadcrumbs([]link{home}, link{Name: v.text.Solutions, URL: data.SolutionsURL})}
	for _, n := range shown {
		articles := byCollection[n.ID]
		card := collectionCard{Name: n.Name, URL: v.collectionURL(n), Count: n.Published, Articles: articles, Sub: n.parent != nil,
			More: n.Published > len(articles)}
		if card.More {
			card.MoreLabel = fmt.Sprintf(v.text.AllN, v.count(n.Published))
		}
		data.Collections = append(data.Collections, card)
	}
	paths := pagePaths{"": CollectionPath(nil)}
	for lang := range shownIn(t.roots) {
		paths[lang] = CollectionPath(nil)
	}
	v.languages(&data, paths, true)
	s.writePage(response, request, v, http.StatusOK, s.pages.render("solutions", data))
	s.viewed(request, v, pageview.Collection, data, "", "")
}
