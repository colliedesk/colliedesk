// Package pageview says which help center requests come from a reader and carries the page-view
// event the help center emits after serving one. Every help center counter uses IsReader, so the
// article views on Results and any analytics a workspace connects count the same visits
// (docs/internals/knowledge-base.md › Views and uses).
package pageview

import (
	"net/http"
	"strings"
	"time"
)

// Kind is the kind of public page a reader viewed.
type Kind string

// The pages that emit an event.
const (
	Home       Kind = "home"
	Collection Kind = "collection"
	Article    Kind = "article"
	Search     Kind = "search"
)

// Event is one page a reader was served. Client and UserAgent are for telling readers apart in
// memory; a consumer must not store them.
type Event struct {
	WorkspaceID string
	// HelpCenterID is the help center that served the page, "" for the workspace's primary.
	HelpCenterID string
	// Timezone is the workspace's IANA timezone, which names the day a view belongs to.
	Timezone string
	Kind     Kind
	// Address is the page's public address, its path for a help center without a host name.
	Address  string
	Title    string
	Referrer string
	// Search is the cleaned search words of a search page.
	Search string
	Locale string
	// ArticleID is the article of an article page, "" for every other kind.
	ArticleID string
	Client    string
	UserAgent string
	At        time.Time
}

// Consumer receives the page-view events. PageViewed runs on the request's goroutine after the
// page is written, so it must not block: it adds to memory and returns.
type Consumer interface {
	PageViewed(Event)
}

// Fanout gives each event to every consumer in order; a nil one is skipped.
type Fanout []Consumer

// PageViewed implements Consumer.
func (f Fanout) PageViewed(event Event) {
	for _, consumer := range f {
		if consumer != nil {
			consumer.PageViewed(event)
		}
	}
}

// Visit is what IsReader judges a request by once it was answered.
type Visit struct {
	Method string
	Status int
	Header http.Header
	// Preview is true for a page a team member renders in the app (the theme preview), which no reader sees.
	Preview bool
}

// IsReader reports whether a served request was a reader's: a GET answered 200, not a prefetch or
// prerender (Sec-Purpose, or the older Purpose header), not a team preview, and with a user agent
// that is not a known bot, crawler, link preview or script. A request without a user agent is not
// a reader's.
func IsReader(visit Visit) bool {
	if visit.Method != http.MethodGet || visit.Status != http.StatusOK || visit.Preview {
		return false
	}
	if prefetch(visit.Header.Get("Sec-Purpose")) || prefetch(visit.Header.Get("Purpose")) {
		return false
	}
	return !IsBot(visit.Header.Get("User-Agent"))
}

func prefetch(purpose string) bool {
	return strings.Contains(strings.ToLower(purpose), "prefetch")
}
