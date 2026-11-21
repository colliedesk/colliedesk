package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// kbFilterFixture is a workspace in Asia/Ho_Chi_Minh (UTC+7) with five articles whose every filtered
// field is set by hand, so each list filter has rows it must return and rows it must leave out.
type kbFilterFixture struct {
	api                        http.Handler
	admin, writerID, adminID   string
	billing, refunds, shipping string
	// The articles by name: card (A), invoice (B), parcel (C), label (D), lost (E).
	ids map[string]string
	// Another workspace's author and collection.
	foreignAgentID, foreignCollectionID string
	pool                                *pgxpool.Pool
}

func newKBFilterFixture(t *testing.T) kbFilterFixture {
	t.Helper()
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "KB Filters", "admin@kb-filters.example.com")
	writer := agentSession(t, api, admin, "writer@kb-filters.example.com")
	f := kbFilterFixture{api: api, pool: pool, admin: admin, writerID: agentID(t, api, writer), adminID: agentID(t, api, admin), ids: map[string]string{}}
	kbCall(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "Asia/Ho_Chi_Minh"}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)

	f.billing = kbCollection(t, api, admin, map[string]any{"name": "Billing"}).ID
	f.refunds = kbCollection(t, api, admin, map[string]any{"name": "Refunds", "parent_id": f.billing}).ID
	deep := kbCollection(t, api, admin, map[string]any{"name": "Card refunds", "parent_id": f.refunds}).ID
	f.shipping = kbCollection(t, api, admin, map[string]any{"name": "Shipping"}).ID

	f.ids["card"] = kbArticle(t, api, admin, map[string]any{"collection_id": f.refunds, "title": "Card refund", "body_md": "Five days.", "status": "published", "tags": []string{"refund", "billing"}}).ID
	f.ids["invoice"] = kbArticle(t, api, admin, map[string]any{"collection_id": f.billing, "title": "Invoice copy", "body_md": "From settings.", "tags": []string{"Billing"}}).ID
	f.ids["parcel"] = kbArticle(t, api, admin, map[string]any{"collection_ids": []string{f.shipping, deep}, "title": "Tracking a parcel", "body_md": "Use the link.", "status": "published", "tags": []string{"shipping"}}).ID
	f.ids["label"] = kbArticle(t, api, admin, map[string]any{"collection_id": f.shipping, "title": "Return label", "body_md": "Print it."}).ID
	f.ids["lost"] = kbArticle(t, api, admin, map[string]any{"collection_id": f.shipping, "title": "Lost package", "body_md": "Tell us."}).ID

	for _, name := range []string{"card", "parcel"} {
		kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+f.ids[name], map[string]any{"updated_at": kbGetArticle(t, api, admin, f.ids[name]).UpdatedAt, "author_agent_id": f.writerID}, admin, http.StatusOK, nil)
	}
	for _, name := range []string{"card", "invoice"} {
		kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+f.ids[name]+"/translations", map[string]any{"locale": "fr", "title": "Traduction", "body_md": "x"}, admin, http.StatusCreated, nil)
	}
	kbEditBody(t, api, admin, f.ids["parcel"], "Use the tracking link.")
	kbRequestReview(t, api, admin, f.ids["label"], f.writerID)
	kbDecide(t, api, writer, kbRequestReview(t, api, admin, f.ids["lost"], f.writerID).ID, "approve", map[string]any{}, http.StatusOK)

	// Times by hand: card at 00:30 on 21 November in the workspace (17:30 UTC the day before), invoice at
	// 23:30 on 20 November, label at 00:30 on 1 November (still 31 October in UTC). The parcel's own row
	// is old, its unpublished changes are recent: updated_at is the later of the two.
	fixtureSQL(t, pool, `UPDATE kb_articles SET created_at=v.created::timestamptz, updated_at=v.updated::timestamptz FROM (VALUES
		($1::uuid,'2026-11-20T17:30:00Z','2026-11-10T00:00:00Z'), ($2::uuid,'2026-11-20T16:30:00Z','2026-11-12T00:00:00Z'),
		($3::uuid,'2026-10-15T03:00:00Z','2026-10-20T00:00:00Z'), ($4::uuid,'2026-10-31T17:30:00Z','2026-10-01T00:00:00Z'),
		($5::uuid,'2026-10-02T00:00:00Z','2026-10-02T00:00:00Z')) v(id,created,updated) WHERE kb_articles.id=v.id`,
		f.ids["card"], f.ids["invoice"], f.ids["parcel"], f.ids["label"], f.ids["lost"])
	fixtureSQL(t, pool, `UPDATE kb_article_drafts SET updated_at='2026-11-15T00:00:00Z' WHERE article_id=$1 AND locale IS NULL`, f.ids["parcel"])
	fixtureSQL(t, pool, `UPDATE kb_article_translations SET outdated=true, outdated_at=now() WHERE article_id=$1`, f.ids["card"])

	foreign := createSession(t, api, service, "KB Filters Elsewhere", "admin@kb-filters-elsewhere.example.com")
	f.foreignAgentID = agentID(t, api, foreign)
	f.foreignCollectionID = kbCollection(t, api, foreign, map[string]any{"name": "Billing"}).ID
	kbArticle(t, api, foreign, map[string]any{"collection_id": f.foreignCollectionID, "title": "Card refund", "body_md": "Elsewhere.", "tags": []string{"billing"}})
	return f
}

func fixtureSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// names lists the fixture articles a query returns, by name, in the fixture's order.
func (f kbFilterFixture) names(t *testing.T, query string) []string {
	t.Helper()
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles?"+query, nil, f.admin, http.StatusOK, &page)
	returned := map[string]bool{}
	for _, item := range page.Items {
		returned[item.ID] = true
	}
	names := []string{}
	for _, name := range []string{"card", "invoice", "parcel", "label", "lost"} {
		if returned[f.ids[name]] {
			names = append(names, name)
		}
	}
	if len(names) != len(page.Items) {
		t.Fatalf("%s returned %d articles, %d of them from the fixture", query, len(page.Items), len(names))
	}
	return names
}
