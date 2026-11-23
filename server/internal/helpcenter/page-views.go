package helpcenter

import (
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// viewed emits the page-view event of a page just served with 200, when a reader asked for it
// (pageview.IsReader): never for a bot, a prefetch, a HEAD or a signed-in team member. articleID is
// the article of an article page and search the words of a search page; both are "" otherwise.
func (s *Site) viewed(request *http.Request, v view, kind pageview.Kind, data page, articleID, search string) {
	if s.config.PageViews == nil {
		return
	}
	if !s.reader(request) {
		return
	}
	address := data.Canonical
	if address == "" {
		address = v.rootAddress + strings.TrimPrefix(request.URL.Path, v.root)
	}
	helpCenterID := ""
	if !v.site.Primary {
		helpCenterID = v.site.HelpCenterID
	}
	s.config.PageViews.PageViewed(pageview.Event{
		WorkspaceID: v.site.WorkspaceID, HelpCenterID: helpCenterID, Timezone: v.site.Timezone, Kind: kind,
		Address: address, Title: data.Title, Referrer: request.Referer(), Search: search, Locale: v.locale(),
		ArticleID: articleID, Client: s.client(request), UserAgent: request.UserAgent(), At: time.Now(),
	})
}
