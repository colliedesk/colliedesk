package helpcenter

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"html/template"
	"log"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// renderVersion changes when rendering changes, so a cached article is never served as an older
// renderer wrote it.
const renderVersion = "1"

// renderWait is how long a visitor waits for an article to render before getting its text as it
// is. Articles render in 1–20 ms, so only a text built to be slow reaches it.
const renderWait = time.Second

// renderCacheBytes bounds the rendered articles kept in memory.
const renderCacheBytes = 64 << 20

// rendered is an article page's text and meta description.
type rendered struct {
	body        template.HTML
	description string
	// plain is a page that shows the text as written because rendering took too long; it must not
	// be cached by the visitor's browser under the page's ETag.
	plain bool
}

func (r rendered) size() int { return len(r.body) + len(r.description) }

// renders renders article pages at most once per text and keeps Markdown from taking the server's
// CPU: goldmark cannot be stopped, and some texts take it seconds (docs/internals/help-center.md).
//   - A rendered page is kept, least recently used out first, by a key of the renderer version,
//     workspace, article, address and text, so a page is never served for another workspace or an
//     older text.
//   - At most slots renders run at once on the server and one per workspace, so one workspace's
//     texts cannot hold every slot.
//   - Visitors of a text being rendered wait for that render (single flight).
//   - A visitor waits at most wait, then gets the text escaped as it is; the render goes on and
//     keeps its page for the next visitor.
type renders struct {
	render func(body, base string) (rendered, error)
	slots  chan struct{}
	wait   time.Duration
	limit  int

	mu      sync.Mutex
	order   *list.List
	pages   map[string]*list.Element
	bytes   int
	flights map[string]*flight
	// busy holds, for a workspace with a render running, a channel closed when it ends.
	busy map[string]chan struct{}
}

type cached struct {
	key  string
	page rendered
}

type flight struct {
	done chan struct{}
	page rendered
	ok   bool
}

func newRenders(render func(body, base string) (rendered, error), slots int, wait time.Duration, limit int) *renders {
	return &renders{render: render, slots: make(chan struct{}, slots), wait: wait, limit: limit,
		order: list.New(), pages: map[string]*list.Element{}, flights: map[string]*flight{}, busy: map[string]chan struct{}{}}
}

// defaultRenders renders with renderArticle, a quarter of the processors at once (at least two).
func defaultRenders() *renders {
	return newRenders(func(body, base string) (rendered, error) {
		page, err := renderArticle(body, base)
		if err != nil {
			return rendered{}, err
		}
		return rendered{body: page, description: excerpt(body, descriptionChars)}, nil
	}, max(2, runtime.GOMAXPROCS(0)/4), renderWait, renderCacheBytes)
}

func renderKey(workspaceID string, number int64, base, body string) string {
	sum := sha256.Sum256([]byte(renderVersion + "\x00" + workspaceID + "\x00" + strconv.FormatInt(number, 10) + "\x00" + base + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

// article returns the page of an article: rendered, or its text escaped as it is when rendering
// would keep the visitor waiting.
func (r *renders) article(workspaceID string, number int64, base, body string) rendered {
	key := renderKey(workspaceID, number, base, body)
	deadline := time.NewTimer(r.wait)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		if element, ok := r.pages[key]; ok {
			r.order.MoveToFront(element)
			page := element.Value.(*cached).page
			r.mu.Unlock()
			return page
		}
		if running, ok := r.flights[key]; ok {
			r.mu.Unlock()
			return r.await(running, deadline, workspaceID, number, body)
		}
		if free, ok := r.busy[workspaceID]; ok {
			// Another article of this workspace renders: wait for it, so that a workspace takes one
			// slot at most.
			r.mu.Unlock()
			select {
			case <-free:
				continue
			case <-deadline.C:
				return plainPage(body)
			}
		}
		free := make(chan struct{})
		running := &flight{done: make(chan struct{})}
		r.busy[workspaceID], r.flights[key] = free, running
		r.mu.Unlock()
		select {
		case r.slots <- struct{}{}:
		case <-deadline.C:
			r.finish(key, workspaceID, running)
			return plainPage(body)
		}
		go func() {
			page, err := r.render(body, base)
			<-r.slots
			if err != nil {
				log.Printf("help center: render an article of workspace %s: %v", workspaceID, err)
			} else {
				running.page, running.ok = page, true
			}
			r.finish(key, workspaceID, running)
		}()
		return r.await(running, deadline, workspaceID, number, body)
	}
}

func (r *renders) await(running *flight, deadline *time.Timer, workspaceID string, number int64, body string) rendered {
	select {
	case <-running.done:
		if running.ok {
			return running.page
		}
	case <-deadline.C:
		log.Printf("help center: article %d of workspace %s still renders after %s; its text is shown as it is", number, workspaceID, r.wait)
	}
	return plainPage(body)
}

// finish ends a render: its page is kept when there is one, and the workspace may render again.
func (r *renders) finish(key, workspaceID string, running *flight) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.flights, key)
	close(r.busy[workspaceID])
	delete(r.busy, workspaceID)
	if running.ok {
		r.keep(key, running.page)
	}
	close(running.done)
}

// keep caches page, dropping the least recently used pages over the limit. A page larger than an
// eighth of the limit is not kept.
func (r *renders) keep(key string, page rendered) {
	if page.size() > r.limit/8 {
		return
	}
	r.pages[key] = r.order.PushFront(&cached{key: key, page: page})
	r.bytes += page.size()
	for r.bytes > r.limit {
		oldest := r.order.Back()
		entry := oldest.Value.(*cached)
		r.order.Remove(oldest)
		delete(r.pages, entry.key)
		r.bytes -= entry.page.size()
	}
}

// plainPage shows an article's text escaped, as written.
func plainPage(body string) rendered {
	description := strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(description) > descriptionChars {
		description = string([]rune(description)[:descriptionChars]) + "…"
	}
	return rendered{body: template.HTML(`<pre class="plain-text">` + html.EscapeString(body) + `</pre>`), description: description, plain: true} //nolint:gosec // escaped
}
