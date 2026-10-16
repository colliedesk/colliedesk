package httpapi_test

import (
	"net/http"
	"strings"
)

const productsPrefix = "/v1/products/"

// serveProducts answers the subscription count and the product read and archive calls. The caller
// holds the fake's lock. It reports whether the request was one of those.
func (f *fakePolarAPI) serveProducts(request *http.Request, reply func(int, string, ...any)) bool {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v1/subscriptions/":
		reply(http.StatusOK, `{"items":[],"pagination":{"total_count":%d,"max_page":1}}`, f.holders[request.URL.Query().Get("product_id")])
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, productsPrefix):
		id := strings.TrimPrefix(request.URL.Path, productsPrefix)
		reply(http.StatusOK, `{"id":%q,"is_archived":%t}`, id, f.archived[id])
	case request.Method == http.MethodPatch && strings.HasPrefix(request.URL.Path, productsPrefix):
		id := strings.TrimPrefix(request.URL.Path, productsPrefix)
		if f.archived == nil {
			f.archived = map[string]bool{}
		}
		f.archived[id] = true
		reply(http.StatusOK, `{"id":%q,"is_archived":true}`, id)
	default:
		return false
	}
	return true
}

// setHolders sets how many subscriptions of a product hold a slot at the fake Polar.
func (f *fakePolarAPI) setHolders(product string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holders == nil {
		f.holders = map[string]int{}
	}
	f.holders[product] = count
}

// setArchived sets the archive flag of a product, as an owner clicking Archive in Polar would.
func (f *fakePolarAPI) setArchived(product string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.archived == nil {
		f.archived = map[string]bool{}
	}
	f.archived[product] = true
}

func (f *fakePolarAPI) isArchived(product string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.archived[product]
}
