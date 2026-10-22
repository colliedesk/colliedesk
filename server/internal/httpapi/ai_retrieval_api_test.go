package httpapi_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// buildAIRetrieval builds every queued source, as the server's worker does.
func buildAIRetrieval(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := store.New(pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatalf("build AI retrieval: %v", err)
	}
}

// aiRetrieve searches in the workspace's own scoped transaction, as an AI task does.
func aiRetrieve(t *testing.T, pool *pgxpool.Pool, workspace string, query store.AIRetrievalQuery) []store.AIRetrievedDoc {
	t.Helper()
	return aiRetrieveAs(t, pool, workspace, workspace, query)
}

// aiRetrieveAs searches for the workspace named in the call inside the scope of another, to prove
// row-level security alone keeps the answer to the scope.
func aiRetrieveAs(t *testing.T, pool *pgxpool.Pool, scope, workspace string, query store.AIRetrievalQuery) []store.AIRetrievedDoc {
	t.Helper()
	var docs []store.AIRetrievedDoc
	err := database.New(pool).InWorkspace(context.Background(), scope, func(tx pgx.Tx) error {
		var err error
		docs, err = store.AIRetrieve(context.Background(), tx, workspace, query)
		return err
	})
	if err != nil {
		t.Fatalf("AI retrieve: %v", err)
	}
	return docs
}

func retrievedTitles(docs []store.AIRetrievedDoc) []string {
	titles := []string{}
	for _, doc := range docs {
		titles = append(titles, doc.Kind+":"+doc.Title)
	}
	return titles
}

// bigKB keeps the workspace's knowledge base above the size that is sent whole, so a search ranks.
func bigKB(t *testing.T, api http.Handler, token, collection string) {
	t.Helper()
	filler := strings.Repeat("Unrelated filler text about the shipping of parcels and warehouses, to and from the depot in time for the day on the road with care. ", 150)
	for _, title := range []string{"Shipping times", "Warehouse hours", "Parcel sizes", "Customs forms"} {
		kbArticle(t, api, token, map[string]any{"collection_id": collection, "title": title, "body_md": filler, "status": "published"})
	}
}

func TestAIRetrievalFindsPublishedArticlesWithoutAccentsAndInEveryLanguage(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval KB", "admin@retrieval-kb.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	bigKB(t, api, admin, collection)
	login := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Đăng nhập bằng Google", "body_md": "Bấm nút Google để đăng nhập.", "status": "published"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Đăng nhập nháp", "body_md": "Bài nháp về đăng nhập.", "status": "draft"})
	refund := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Refunds", "body_md": "We refund within five days.", "status": "published"})
	if _, err := pool.Exec(context.Background(), `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) VALUES($1,$2,'de','Rückerstattung','Wir erstatten innerhalb von fünf Tagen.')`,
		workspace, refund.ID); err != nil {
		t.Fatal(err)
	}
	buildAIRetrieval(t, pool)

	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "Toi khong dang nhap duoc", KBLimit: 5})
	if len(got) == 0 || got[0].KBArticleID != login.ID || got[0].Number != login.Number || got[0].Title != "Đăng nhập bằng Google" {
		t.Fatalf("unaccented query = %v, want the published login article first", retrievedTitles(got))
	}
	if slices.Contains(retrievedTitles(got), "kb_article:Đăng nhập nháp") {
		t.Errorf("a draft reached AI: %v", retrievedTitles(got))
	}
	german := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "Rückerstattung bitte", KBLimit: 5})
	if len(german) == 0 || german[0].KBArticleID != refund.ID || german[0].KBLocale != "de" || !strings.Contains(german[0].Text, "fünf Tagen") {
		t.Fatalf("German query = %+v, want the German translation of the refund article", german)
	}
	// One article fills one place, in its best language.
	both := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "refund Rückerstattung", KBLimit: 5})
	count := 0
	for _, doc := range both {
		if doc.KBArticleID == refund.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("refund article returned %d times: %v", count, retrievedTitles(both))
	}

	// An edit is found after the next build; unpublishing hides it at once.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+login.ID, map[string]any{"body_md": "Dùng mật khẩu một lần.", "updated_at": login.UpdatedAt}, admin, http.StatusOK, &login)
	buildAIRetrieval(t, pool)
	if got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "mat khau", KBLimit: 5}); len(got) == 0 || got[0].KBArticleID != login.ID {
		t.Errorf("edited text not found: %v", retrievedTitles(got))
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+login.ID, map[string]any{"status": "draft", "updated_at": login.UpdatedAt}, admin, http.StatusOK, &login)
	for _, doc := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "dang nhap mat khau", KBLimit: 5}) {
		if doc.KBArticleID == login.ID {
			t.Errorf("an unpublished article reached AI before the next build")
		}
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+refund.ID+"?updated_at="+refund.UpdatedAt, nil, admin, http.StatusNoContent, nil)
	for _, doc := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "refund Rückerstattung", KBLimit: 5}) {
		if doc.KBArticleID == refund.ID {
			t.Errorf("an article in the trash reached AI")
		}
	}
}

func TestAIRetrievalRanksRareWordsAboveCommonOnes(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Rank", "admin@retrieval-rank.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	bigKB(t, api, admin, collection)
	common := "the of a and to in for on with the of a and to in for on with "
	kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "The basics of the account", "body_md": common + common + "the account of the team", "status": "published"})
	refund := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Money back", "body_md": "Refund rules.", "status": "published"})
	buildAIRetrieval(t, pool)
	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "the refund of the a and to in for the of", KBLimit: 5})
	if len(got) == 0 || got[0].KBArticleID != refund.ID {
		t.Fatalf("ranking = %v, want the only article with the rare word first", retrievedTitles(got))
	}
}

func TestAIRetrievalSendsASmallKnowledgeBaseWhole(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Small", "admin@retrieval-small.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Opening hours", "body_md": "Nine to five.", "status": "published"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Refunds", "body_md": "Within five days.", "status": "published"})
	buildAIRetrieval(t, pool)
	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "zzzz nothing matches", KBLimit: 1})
	if len(got) != 2 {
		t.Fatalf("small knowledge base = %v, want both articles whatever the query", retrievedTitles(got))
	}
}

// seedResolvedConversation makes a conversation with a customer message, a private note and a reply, resolved.
func seedResolvedConversation(t *testing.T, api http.Handler, pool *pgxpool.Pool, token, workspace, contact, subject, incoming, note, reply string) string {
	t.Helper()
	conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": subject})
	if _, err := pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'incoming',$3,now()-interval '1 hour')`,
		workspace, conversation, incoming); err != nil {
		t.Fatal(err)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": note}, token, http.StatusCreated, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": reply}, token, http.StatusCreated, nil)
	kbCall(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "resolved"}, token, http.StatusOK, nil)
	return conversation
}

func TestAIRetrievalReadsResolvedConversationsWithoutNotesOrTheCustomersDetails(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Tickets", "admin@retrieval-tickets.example.com")
	workspace := workspaceID(t, api, admin)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Nguyễn Thị Hoa", "email": "hoa.nguyen@customer.example"})
	if _, err := pool.Exec(context.Background(), `UPDATE contacts SET organization='Hoa Lan Trading' WHERE id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	resolved := seedResolvedConversation(t, api, pool, admin, workspace, contact, "Máy in kẹt giấy",
		"Chào, tôi là Nguyễn Thị Hoa ở Hoa Lan Trading, máy in kẹt giấy. Gọi tôi 090 123 4567 hoặc +84 90-123-4567, email hoa.nguyen@customer.example, bạn tôi lan@other.example.",
		"NOTE-CANARY internal: customer is on the old plan",
		"Chị Hoa thân mến, hãy mở khay giấy và lấy tờ bị kẹt ra.")
	open := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Máy in kẹt giấy lần nữa"})
	buildAIRetrieval(t, pool)

	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "may in ket giay", ConversationLimit: 3})
	var doc *store.AIRetrievedDoc
	for i := range got {
		if got[i].Kind == "conversation" {
			if got[i].ConversationID == open {
				t.Errorf("an open conversation reached AI")
			}
			if got[i].ConversationID == resolved {
				doc = &got[i]
			}
		}
	}
	if doc == nil {
		t.Fatalf("resolved conversation not found: %v", retrievedTitles(got))
	}
	text := doc.Title + "\n" + doc.Text
	for _, leak := range []string{"NOTE-CANARY", "Nguyễn", "Hoa", "hoa.nguyen@customer.example", "lan@other.example", "Hoa Lan Trading", "090 123 4567", "90-123-4567"} {
		if strings.Contains(text, leak) {
			t.Errorf("retrieved text holds %q:\n%s", leak, text)
		}
	}
	for _, kept := range []string{"kẹt giấy", "khay giấy", "[customer]", "[email]", "[number]", "[company]"} {
		if !strings.Contains(text, kept) {
			t.Errorf("retrieved text lacks %q:\n%s", kept, text)
		}
	}
	// The conversation being answered is never its own source.
	for _, other := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "may in ket giay", ConversationLimit: 3, ExcludeConversationID: resolved}) {
		if other.ConversationID == resolved {
			t.Errorf("the excluded conversation came back")
		}
	}
	// Reopening takes it away at once.
	kbCall(t, api, http.MethodPatch, "/api/v1/conversations/"+resolved, map[string]any{"status": "open"}, admin, http.StatusOK, nil)
	for _, other := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "may in ket giay", ConversationLimit: 3}) {
		if other.ConversationID == resolved {
			t.Errorf("a reopened conversation reached AI")
		}
	}
	var rows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ai_retrieval_docs WHERE conversation_id=$1`, resolved).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("reopened conversation keeps %d retrieval rows (%v)", rows, err)
	}
}

// An import writes the conversation, already resolved, before its messages, each in its own transaction.
func TestAIRetrievalBuildsAnImportedConversationOnceItsMessagesLand(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Import", "admin@retrieval-import.example.com")
	workspace := workspaceID(t, api, admin)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Imported Person", "email": "imported@customer.example"})
	ctx := context.Background()
	var conversation string
	if err := pool.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,origin,created_at,updated_at)
		VALUES($1,$2,'Legacy invoice question','resolved','{"source":"freshdesk","id":"1"}','2024-01-01','2024-01-02') RETURNING id`, workspace, contact).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	buildAIRetrieval(t, pool)
	for _, kind := range []string{"incoming", "outgoing"} {
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin,created_at) VALUES($1,$2,$3,$4,'{"source":"freshdesk"}','2024-01-01')`,
			workspace, conversation, kind, "Legacy invoice "+kind+" text"); err != nil {
			t.Fatal(err)
		}
	}
	buildAIRetrieval(t, pool)
	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "legacy invoice", ConversationLimit: 3})
	if len(got) != 1 || got[0].ConversationID != conversation || !strings.Contains(got[0].Text, "Legacy invoice incoming text") || !strings.Contains(got[0].Text, "Legacy invoice outgoing text") {
		t.Fatalf("imported conversation = %+v, want it with its customer message and reply", got)
	}
}

// A change that commits while a build runs is built by the next one, never dropped.
func TestAIRetrievalKeepsAChangeThatCommitsDuringABuild(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Race", "admin@retrieval-race.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	bigKB(t, api, admin, collection)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Printer", "body_md": "Old words only.", "status": "published"})
	ctx := context.Background()
	edit, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Exec(ctx, `UPDATE kb_articles SET body_md='Brand new wording.' WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	buildAIRetrieval(t, pool) // sees the old text; the edit's queue row is not committed yet
	if err := edit.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	buildAIRetrieval(t, pool)
	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "brand new wording", KBLimit: 5})
	if len(got) == 0 || got[0].KBArticleID != article.ID || !strings.Contains(got[0].Text, "Brand new wording") {
		t.Fatalf("after the race = %+v, want the new text", got)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ai_retrieval_pending WHERE workspace_id=$1`, workspace).Scan(&pending); err != nil || pending != 0 {
		t.Errorf("queue keeps %d rows after the builds (%v)", pending, err)
	}
}

func TestAIRetrievalNeverReturnsAnotherWorkspacesText(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "Retrieval A", "admin@retrieval-a.example.com")
	adminB := createSession(t, api, service, "Retrieval B", "admin@retrieval-b.example.com")
	a, b := workspaceID(t, api, adminA), workspaceID(t, api, adminB)
	const canary = "zebracanary"
	for _, side := range []struct{ token, workspace, word string }{{adminA, a, "printer"}, {adminB, b, canary + " printer"}} {
		collection := kbCollection(t, api, side.token, map[string]any{"name": "Help"}).ID
		article := kbArticle(t, api, side.token, map[string]any{"collection_id": collection, "title": "Article " + side.word, "body_md": side.word, "status": "published"})
		if _, err := pool.Exec(context.Background(), `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) VALUES($1,$2,'fr',$3,$3)`,
			side.workspace, article.ID, "Traduction "+side.word); err != nil {
			t.Fatal(err)
		}
		contact := create(t, api, side.token, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat@" + side.workspace[:8] + ".example"})
		seedResolvedConversation(t, api, pool, side.token, side.workspace, contact, "Ticket "+side.word, "Help with "+side.word, "note", "Answer about "+side.word)
	}
	buildAIRetrieval(t, pool)
	query := store.AIRetrievalQuery{Text: canary + " printer", KBLimit: 10, ConversationLimit: 10}
	got := aiRetrieve(t, pool, a, query)
	if len(got) == 0 {
		t.Fatal("workspace A found nothing of its own")
	}
	for _, doc := range got {
		if strings.Contains(doc.Title+doc.Text, canary) {
			t.Errorf("workspace A retrieved workspace B's %s %q", doc.Kind, doc.Title)
		}
	}
	// Naming workspace B inside workspace A's scope: row-level security alone answers nothing.
	if leaked := aiRetrieveAs(t, pool, a, b, query); len(leaked) != 0 {
		t.Errorf("workspace A's scope returned %d documents for workspace B: %v", len(leaked), retrievedTitles(leaked))
	}
	if own := aiRetrieve(t, pool, b, query); len(own) == 0 {
		t.Error("workspace B does not find its own documents")
	}
}

func TestExcludingAnArticleFromAIIsAVersionThatKeepsTheTextAndCanBeUndone(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Exclude", "admin@retrieval-exclude.example.com")
	other := createSession(t, api, service, "Retrieval Exclude Other", "admin@retrieval-exclude-other.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	bigKB(t, api, admin, collection)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Internal refund rules", "body_md": "Refunds above 100 need approval.", "status": "published"})
	if _, err := pool.Exec(context.Background(), `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) VALUES($1,$2,'de','Erstattung','Erstattung Regeln')`,
		workspace, article.ID); err != nil {
		t.Fatal(err)
	}
	buildAIRetrieval(t, pool)
	found := func(text string) bool {
		for _, doc := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: text, KBLimit: 5}) {
			if doc.KBArticleID == article.ID {
				return true
			}
		}
		return false
	}
	if !found("refund rules") || !found("Erstattung") {
		t.Fatal("article not found before it was excluded")
	}
	path := "/api/v1/kb/articles/" + article.ID + "/ai-exclusion"
	// Another workspace's admin cannot see or change it.
	kbCall(t, api, http.MethodPost, path, nil, other, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodDelete, path, nil, other, http.StatusNotFound, nil)

	var excluded struct {
		kbArticleView
		AIExclusion *struct {
			At string `json:"at"`
			By *struct {
				Name string `json:"name"`
			} `json:"by"`
		} `json:"ai_exclusion"`
	}
	kbCall(t, api, http.MethodPost, path, nil, admin, http.StatusOK, &excluded)
	if excluded.AIExclusion == nil || excluded.AIExclusion.By == nil || excluded.AIExclusion.By.Name == "" {
		t.Fatalf("ai_exclusion = %+v, want since when and by whom", excluded.AIExclusion)
	}
	if excluded.UpdatedAt != article.UpdatedAt || excluded.BodyMD != article.BodyMD {
		t.Errorf("excluding changed the article: updated_at %s → %s", article.UpdatedAt, excluded.UpdatedAt)
	}
	if excluded.LastChange == nil || excluded.LastChange.Kind != "ai_exclude" {
		t.Errorf("last change = %+v, want an ai_exclude version", excluded.LastChange)
	}
	// At once, in every language, without waiting for a build.
	if found("refund rules") || found("Erstattung") {
		t.Error("an excluded article reached AI")
	}
	// An edit opened before the exclusion still saves.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"body_md": "Refunds above 200 need approval.", "updated_at": article.UpdatedAt}, admin, http.StatusOK, nil)
	buildAIRetrieval(t, pool)
	if found("refund rules") {
		t.Error("an excluded article reached AI after an edit")
	}
	// Again changes nothing; no second version.
	var versions struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodPost, path, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID+"/versions", nil, admin, http.StatusOK, &versions)
	kinds := []string{}
	for _, version := range versions.Items {
		kinds = append(kinds, version.Kind)
	}
	if strings.Join(kinds, ",") != "edit,ai_exclude,create" {
		t.Errorf("versions = %v, want edit, ai_exclude, create", kinds)
	}

	var included struct {
		AIExclusion *struct{}        `json:"ai_exclusion"`
		LastChange  *replyChangeView `json:"last_change"`
	}
	kbCall(t, api, http.MethodDelete, path, nil, admin, http.StatusOK, &included)
	if included.AIExclusion != nil || included.LastChange == nil || included.LastChange.Kind != "ai_include" {
		t.Errorf("after including: ai_exclusion=%v last change=%+v", included.AIExclusion, included.LastChange)
	}
	buildAIRetrieval(t, pool)
	if !found("refund rules") || !found("Erstattung") {
		t.Error("an article AI may read again is not found")
	}
	kbCall(t, api, http.MethodDelete, path, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/00000000-0000-4000-8000-000000000000/ai-exclusion", nil, admin, http.StatusNotFound, nil)
}

// A name's words alone are masked only as a name is written, so English text about a customer named
// Trần Văn Long keeps "long" and "an"; the names of contacts merged into the customer are masked too.
func TestAIRetrievalMasksNamesWithoutEatingWords(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Names", "admin@retrieval-names.example.com")
	workspace := workspaceID(t, api, admin)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Trần Văn Long", "email": "long@customer.example"})
	if _, err := pool.Exec(context.Background(), `INSERT INTO contacts(workspace_id,name,email,merged_into_id) VALUES($1,'Duplicate Person','dup@customer.example',$2)`,
		workspace, contact); err != nil {
		t.Fatal(err)
	}
	conversation := seedResolvedConversation(t, api, pool, admin, workspace, contact, "Export takes too long",
		"Hi, Long here (also writing as Duplicate Person). The export takes too long and an error shows. Regards, Tran Van Long",
		"note", "Hello Long, an export of that size takes a while; it is fixed now.")
	buildAIRetrieval(t, pool)
	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "export takes too long error", ConversationLimit: 3})
	if len(got) != 1 || got[0].ConversationID != conversation {
		t.Fatalf("search = %+v", got)
	}
	text := got[0].Title + "\n" + got[0].Text
	for _, leak := range []string{"Long here", "Hello Long", "Tran Van Long", "Duplicate Person", "Duplicate"} {
		if strings.Contains(text, leak) {
			t.Errorf("retrieved text holds %q:\n%s", leak, text)
		}
	}
	for _, kept := range []string{"takes too long", "and an error", "an export"} {
		if !strings.Contains(text, kept) {
			t.Errorf("retrieved text lost %q:\n%s", kept, text)
		}
	}
}
