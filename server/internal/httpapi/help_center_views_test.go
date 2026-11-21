package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/kbviews"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const browserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// viewsDesk is a help center with one published article, served with a view counter writing to writer.
type viewsDesk struct {
	api     http.Handler
	site    http.Handler
	pool    *pgxpool.Pool
	admin   string
	slug    string
	article kbArticleView
	page    string
	counter *kbviews.Counter
}

func newViewsDesk(t *testing.T, name string, writer func(*pgxpool.Pool) kbviews.Writer) viewsDesk {
	t.Helper()
	pool := testPool(t)
	if writer == nil {
		writer = func(pool *pgxpool.Pool) kbviews.Writer { return store.New(pool) }
	}
	d := viewsDesk{pool: pool, counter: kbviews.New(writer(pool), kbviews.Options{})}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.PageViews = d.counter })
	d.api, d.site = api, helpCenterSite(api)
	d.admin = createSession(t, api, service, name, "admin@"+name+".example.com")
	d.slug = workspaceSlug(t, api, d.admin)
	enableHelpCenter(t, api, d.admin, name+" help", true)
	collection := kbCollection(t, api, d.admin, map[string]any{"name": "Guides"})
	d.article = kbArticle(t, api, d.admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Open Settings."})
	d.page = fmt.Sprintf("/help/%s/a/%d-%s", d.slug, d.article.Number, d.article.Slug)
	return d
}

func (d viewsDesk) flush(t *testing.T) {
	t.Helper()
	if err := d.counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (d viewsDesk) views(t *testing.T, articleID string) int64 {
	t.Helper()
	var views int64
	if err := d.pool.QueryRow(context.Background(), `SELECT COALESCE(sum(views), 0) FROM kb_article_views WHERE kb_article_id=$1`, articleID).Scan(&views); err != nil {
		t.Fatal(err)
	}
	return views
}

func TestABrowserCountsOneArticleViewAndBotsPrefetchesAndTheTeamCountNone(t *testing.T) {
	d := newViewsDesk(t, "views-count", nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.10", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.10", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.11", "User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.12", "User-Agent", browserAgent, "Sec-Purpose", "prefetch"), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.13", "User-Agent", browserAgent, "Cookie", "colliedesk_session="+d.admin), http.StatusOK, nil, nil)
	head := httptest.NewRequest(http.MethodHead, d.page, nil)
	head.RemoteAddr, head.Host = "192.0.2.14:4242", testOriginHost
	head.Header.Set("User-Agent", browserAgent)
	if response := serve(d.site, head); response.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d", response.Code)
	}
	wantPage(t, visit(d.site, "/help/"+d.slug, "192.0.2.15", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/theme/preview", map[string]any{"page": "article", "theme": map[string]any{}}, d.admin, http.StatusOK, nil)
	d.flush(t)
	if got := d.views(t, d.article.ID); got != 1 {
		t.Fatalf("views = %d, want 1: one browser twice, none for a bot, a prefetch, a signed-in team member, a HEAD, the home page or a theme preview", got)
	}
	var day time.Time
	if err := d.pool.QueryRow(context.Background(), `SELECT day FROM kb_article_views WHERE kb_article_id=$1`, d.article.ID).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if today := time.Now().UTC().Format(time.DateOnly); day.Format(time.DateOnly) != today {
		t.Fatalf("day = %s, want %s (the workspace's timezone is UTC)", day.Format(time.DateOnly), today)
	}
}

func TestAnArticleTheHelpCenterDoesNotShowCountsNoView(t *testing.T) {
	d := newViewsDesk(t, "views-hidden", nil)
	collection := kbCollection(t, d.api, d.admin, map[string]any{"name": "Team notes"})
	for _, visibility := range []string{"agents", "help_chat"} {
		hidden := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": collection.ID, "title": "Refund overrides " + visibility, "status": "published", "visibility": visibility, "body_md": "Ask the lead."})
		path := fmt.Sprintf("/help/%s/a/%d-%s", d.slug, hidden.Number, hidden.Slug)
		wantPage(t, visit(d.site, path, "192.0.2.20", "User-Agent", browserAgent), http.StatusNotFound, nil, nil)
		d.flush(t)
		if got := d.views(t, hidden.ID); got != 0 {
			t.Fatalf("%s article views = %d, want 0", visibility, got)
		}
	}
}

// blockedWriter holds every write until release is closed, then writes to the store.
type blockedWriter struct {
	store   *store.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedWriter) WriteKBArticleViews(ctx context.Context, counts []kbviews.Count) error {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return w.store.WriteKBArticleViews(ctx, counts)
}

func TestAnArticlePageNeverWaitsOnTheViewWrite(t *testing.T) {
	writer := &blockedWriter{started: make(chan struct{}), release: make(chan struct{})}
	d := newViewsDesk(t, "views-blocked", func(pool *pgxpool.Pool) kbviews.Writer {
		writer.store = store.New(pool)
		return writer
	})
	wantPage(t, visit(d.site, d.page, "192.0.2.30", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	flushed := make(chan error, 1)
	go func() { flushed <- d.counter.Flush(context.Background()) }()
	<-writer.started
	durations := make([]time.Duration, 20)
	for index := range durations {
		started := time.Now()
		wantPage(t, visit(d.site, d.page, fmt.Sprintf("192.0.2.%d", 40+index), "User-Agent", browserAgent), http.StatusOK, nil, nil)
		durations[index] = time.Since(started)
	}
	for _, took := range durations {
		if took > 100*time.Millisecond {
			t.Fatalf("an article page took %s while the view write was held, want at most 100ms: %v", took, durations)
		}
	}
	close(writer.release)
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if got := d.views(t, d.article.ID); got != 1 {
		t.Fatalf("views after the held write = %d, want 1", got)
	}
	d.flush(t)
	if got := d.views(t, d.article.ID); got != 21 {
		t.Fatalf("views after the next flush = %d, want 21", got)
	}
}

func TestViewCountsAddUpAndAnArticleDeletedMeanwhileIsSkipped(t *testing.T) {
	d := newViewsDesk(t, "views-add", nil)
	var workspaceID string
	if err := d.pool.QueryRow(context.Background(), `SELECT workspace_id::text FROM kb_articles WHERE id=$1`, d.article.ID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	writer := store.New(d.pool)
	counts := []kbviews.Count{{WorkspaceID: workspaceID, ArticleID: d.article.ID, Locale: "en", Day: day, Views: 3},
		{WorkspaceID: workspaceID, ArticleID: "00000000-0000-4000-8000-000000000001", Locale: "en", Day: day, Views: 5}}
	for range 2 {
		if err := writer.WriteKBArticleViews(context.Background(), counts); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.views(t, d.article.ID); got != 6 {
		t.Fatalf("views = %d, want 6", got)
	}
}
