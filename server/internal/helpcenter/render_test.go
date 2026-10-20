package helpcenter

import (
	"html/template"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gate is a render that waits until its text is released, and counts its calls.
type gate struct {
	mu      sync.Mutex
	release map[string]chan struct{}
	calls   atomic.Int32
	started chan string
}

func newGate() *gate {
	return &gate{release: map[string]chan struct{}{}, started: make(chan string, 100)}
}

func (g *gate) hold(body string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.release[body] == nil {
		g.release[body] = make(chan struct{})
	}
	return g.release[body]
}

func (g *gate) render(body, base string) (rendered, error) {
	g.calls.Add(1)
	g.started <- body
	if strings.HasPrefix(body, "slow") {
		<-g.hold(body)
	}
	return rendered{body: template.HTML("<p>" + base + " " + body + "</p>"), description: body}, nil
}

func isPlain(page rendered) bool {
	return strings.HasPrefix(string(page.body), `<pre class="plain-text">`)
}

func TestRendersKeepAPagePerWorkspaceAddressAndText(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, time.Second, 1<<20)
	a := r.article("workspace-a", 1, "/help/a", "same text")
	b := r.article("workspace-b", 1, "/help/b", "same text")
	if a.body != "<p>/help/a same text</p>" || b.body != "<p>/help/b same text</p>" {
		t.Fatalf("pages %q %q", a.body, b.body)
	}
	if again := r.article("workspace-a", 1, "/help/a", "same text"); again.body != a.body || g.calls.Load() != 2 {
		t.Fatalf("again %q after %d renders", again.body, g.calls.Load())
	}
	// Another workspace with the same article number, address and text still gets its own render.
	if other := r.article("workspace-c", 1, "/help/a", "same text"); g.calls.Load() != 3 || other.body != a.body {
		t.Fatalf("workspace c: %d renders", g.calls.Load())
	}
	if edited := r.article("workspace-a", 1, "/help/a", "new text"); edited.body != "<p>/help/a new text</p>" {
		t.Fatalf("edited %q", edited.body)
	}
}

func TestRendersOfOneTextRunOnceForEveryVisitor(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, 5*time.Second, 1<<20)
	release := g.hold("slow one")
	pages := make(chan rendered, 5)
	for range 5 {
		go func() { pages <- r.article("workspace-a", 1, "", "slow one") }()
	}
	<-g.started
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 5 {
		if page := <-pages; isPlain(page) {
			t.Fatalf("a visitor got %q", page.body)
		}
	}
	if calls := g.calls.Load(); calls != 1 {
		t.Fatalf("%d renders", calls)
	}
}

// A slow text shows as it is after the wait, renders once in the background, and its page is kept.
func TestASlowRenderShowsTheTextThenKeepsThePage(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, 50*time.Millisecond, 1<<20)
	release := g.hold("slow <b>text</b>")
	page := r.article("workspace-a", 1, "", "slow <b>text</b>")
	if page.body != `<pre class="plain-text">slow &lt;b&gt;text&lt;/b&gt;</pre>` || page.description != "slow <b>text</b>" {
		t.Fatalf("page %q %q", page.body, page.description)
	}
	close(release)
	for deadline := time.Now().Add(2 * time.Second); ; {
		if page := r.article("workspace-a", 1, "", "slow <b>text</b>"); !isPlain(page) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the page was never kept")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := g.calls.Load(); calls != 1 {
		t.Fatalf("%d renders", calls)
	}
}

// One workspace's slow texts take one slot: another workspace's articles still render.
func TestOneWorkspaceTakesOneRenderSlot(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, 100*time.Millisecond, 1<<20)
	for index, body := range []string{"slow 1", "slow 2", "slow 3"} {
		r.article("attacker", int64(index), "", body)
	}
	if calls := g.calls.Load(); calls != 1 {
		t.Fatalf("the attacker's texts started %d renders", calls)
	}
	started := time.Now()
	if page := r.article("customer", 1, "", "help"); isPlain(page) || time.Since(started) > 50*time.Millisecond {
		t.Fatalf("customer page %q after %s", page.body, time.Since(started))
	}
	close(g.hold("slow 1"))
}

// Every slot held by other workspaces' slow texts: a visitor waits the limit, then reads the text.
func TestFullSlotsShowTheTextAfterTheWait(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, 100*time.Millisecond, 1<<20)
	r.article("first", 1, "", "slow a")
	r.article("second", 1, "", "slow b")
	if page := r.article("third", 1, "", "help"); !isPlain(page) {
		t.Fatalf("page %q", page.body)
	}
	close(g.hold("slow a"))
	close(g.hold("slow b"))
	for deadline := time.Now().Add(2 * time.Second); isPlain(r.article("third", 1, "", "help")); {
		if time.Now().After(deadline) {
			t.Fatal("third never rendered")
		}
	}
}

func TestRenderedPagesStayWithinTheirBytes(t *testing.T) {
	g := newGate()
	r := newRenders(g.render, 2, time.Second, 4000)
	for number := range int64(20) {
		r.article("workspace-a", number, "", strings.Repeat("x", 200))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bytes > 4000 || len(r.pages) != r.order.Len() || len(r.pages) == 0 {
		t.Fatalf("%d bytes in %d pages", r.bytes, len(r.pages))
	}
	if _, ok := r.pages[renderKey("workspace-a", 19, "", strings.Repeat("x", 200))]; !ok {
		t.Fatal("the newest page was dropped")
	}
	if _, ok := r.pages[renderKey("workspace-a", 0, "", strings.Repeat("x", 200))]; ok {
		t.Fatal("the oldest page was kept")
	}
}
