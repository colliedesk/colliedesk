package helpcenter

import (
	"container/list"
	"sync"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// relatedListsKept bounds the related lists kept in memory: five titles and slugs each, a few
// megabytes in all.
const relatedListsKept = 8192

// relatedKey names the related list of one article page on one help center, which lists only what
// that help center shows. The knowledge base's content version is
// part of it: every write to the knowledge base grows it (bumpKB), the same version that every page's
// ETag carries, so a list is never served after an article in it was unpublished, trashed or hidden.
type relatedKey struct {
	workspaceID  string
	helpCenterID string
	version      int64
	lang         string
	number       int64
}

// relatedLists keeps the related articles of the article pages read lately, least recently used out
// first. Ranking reads every shown article's words, which no index serves under row-level security,
// so it runs once per article, language and version of the knowledge base, not on every visit.
type relatedLists struct {
	limit int

	mu    sync.Mutex
	order *list.List
	lists map[relatedKey]*list.Element
}

type relatedEntry struct {
	key      relatedKey
	articles []domain.PublicKBArticle
}

func newRelatedLists(limit int) *relatedLists {
	return &relatedLists{limit: limit, order: list.New(), lists: map[relatedKey]*list.Element{}}
}

// get returns the list kept under key.
func (r *relatedLists) get(key relatedKey) ([]domain.PublicKBArticle, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	element, ok := r.lists[key]
	if !ok {
		return nil, false
	}
	r.order.MoveToFront(element)
	return element.Value.(*relatedEntry).articles, true
}

// keep keeps articles under key, dropping the least recently used lists past the limit.
func (r *relatedLists) keep(key relatedKey, articles []domain.PublicKBArticle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if element, ok := r.lists[key]; ok {
		element.Value.(*relatedEntry).articles = articles
		r.order.MoveToFront(element)
		return
	}
	r.lists[key] = r.order.PushFront(&relatedEntry{key: key, articles: articles})
	for r.order.Len() > r.limit {
		oldest := r.order.Back()
		r.order.Remove(oldest)
		delete(r.lists, oldest.Value.(*relatedEntry).key)
	}
}
