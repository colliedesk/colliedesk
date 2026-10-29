package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// chatSetupBudget bounds the server's work of a chat turn before the provider is called; the reading of
// a chat back by its browser has half of it.
const (
	chatSetupBudget   = 100 * time.Millisecond
	chatSessionBudget = 50 * time.Millisecond
)

// TestAIChatTurnSetupLatencyBudget holds a chat question, from the request to the call of the provider,
// to 100 ms p95 on the knowledge base budget's fixture (1,000 published articles, 20 of them 100,000
// characters long: too large to send whole, so each question searches for excerpts): the channel and
// feature checks, the chat and its turn, the set of articles, its fingerprint, the search and the
// prompt. Reading a chat back by its cookie stays at or below 50 ms p95.
func TestAIChatTurnSetupLatencyBudget(t *testing.T) {
	_, pool, _, slug := sharedKBBudget(t)
	var mu sync.Mutex
	var called time.Time
	fake := &aiprovider.Fake{FakeKind: aiprovider.KindAnthropic, Answer: func(request aiprovider.Request) (aiprovider.Response, error) {
		mu.Lock()
		called = time.Now()
		mu.Unlock()
		return aiprovider.Response{Text: "It ships in 2 days.", Stop: aiprovider.StopDone, Model: request.Model}, nil
	}}
	api, _, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) {
		config.AI = fakeAIConfig(fake)
		config.AI.Server = chatServerProvider()
	})
	ctx := context.Background()
	workspaceID := queryString(t, pool, `SELECT id::text FROM workspaces WHERE slug=$1`, slug)
	// The search reads the AI retrieval rows. TestAIRetrievalLatencyBudget seeds its own on the same fixture,
	// so rows built here go again at the end, whichever gate runs first.
	if queryString(t, pool, `SELECT count(*)::text FROM ai_retrieval_docs WHERE workspace_id=$1`, workspaceID) == "0" {
		if _, err := store.New(pool).BuildAIRetrieval(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `DELETE FROM ai_retrieval_docs WHERE workspace_id IN (SELECT id FROM workspaces WHERE name LIKE 'KB Budget %')`); err != nil {
				t.Errorf("remove the built retrieval rows: %v", err)
			}
		})
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,effort,enabled,consented_at) VALUES($1,'chat',true,'claude-haiku-5-5','low',true,now())
		ON CONFLICT (workspace_id,slot) DO NOTHING`, workspaceID); err != nil {
		t.Fatal(err)
	}
	channel := queryString(t, pool, `WITH c AS (INSERT INTO channels(workspace_id,type,name,is_default) VALUES($1,'help_chat','Budget chat',true) RETURNING id),
		h AS (INSERT INTO help_chat_channels(workspace_id,channel_id,greeting) SELECT $1,id,'Hi' FROM c) SELECT id::text FROM c`, workspaceID)
	t.Cleanup(func() {
		for _, statement := range []string{`DELETE FROM channels WHERE workspace_id=$1 AND type='help_chat'`, `DELETE FROM ai_slots WHERE workspace_id=$1 AND slot='chat'`} {
			if _, err := pool.Exec(ctx, statement, workspaceID); err != nil {
				t.Errorf("remove the budget chat: %v", err)
			}
		}
	})
	d := chatDesk{api: api, site: helpCenterSite(api), pool: pool, fake: fake, slug: slug, channel: channel, ws: workspaceID,
		host: testOriginHost, base: "/help/" + slug, origin: testOrigin}
	const turns = 40
	setup := make([]time.Duration, 0, turns)
	reads := make([]time.Duration, 0, turns)
	word := queryString(t, pool, `SELECT substr(md5('1234'),1,6)`)
	for i := 0; i <= turns; i++ {
		req := newRequest(t, http.MethodPost, d.base+"/chat/turns", map[string]any{"question": fmt.Sprintf("How do I use %s with article %d?", word, i+1)}, "", nil)
		req.Host, req.RemoteAddr = d.host, fmt.Sprintf("198.51.101.%d:4000", i+1)
		req.Header.Set("Origin", d.origin)
		started := time.Now()
		response := serve(d.site, req)
		events := chatEvents(t, response)
		if last := events[len(events)-1]; last.Type != "done" {
			t.Fatalf("turn %d ended with %+v", i, last)
		}
		mu.Lock()
		took := called.Sub(started)
		mu.Unlock()
		cookie := chatCookie(t, response)
		readStarted := time.Now()
		if read := d.call(t, http.MethodGet, "/chat/session", nil, cookie); read.Code != http.StatusOK {
			t.Fatalf("read back status=%d", read.Code)
		}
		// The first turn measures the set of articles once (later turns read that from the database).
		if i > 0 {
			setup, reads = append(setup, took), append(reads, time.Since(readStarted))
		}
	}
	p95 := func(list []time.Duration) time.Duration {
		sort.Slice(list, func(a, b int) bool { return list[a] < list[b] })
		return list[(len(list)*95+99)/100-1]
	}
	// The chat page and an article with its chat box are help center pages: the pages' budget.
	for name, path := range map[string]string{"chat page": d.base + "/chat", "article with the chat box": d.base + "/a/1000-article-1000"} {
		pageP95 := helpCenterP95(t, d.site, path)
		t.Logf("%s p95=%s", name, pageP95)
		if pageP95 > inboxLatencyBudget {
			t.Errorf("%s p95 %s exceeds %s", name, pageP95, inboxLatencyBudget)
		}
	}
	setupP95, readP95 := p95(setup), p95(reads)
	t.Logf("chat turn setup p95=%s, chat read p95=%s", setupP95, readP95)
	if setupP95 > chatSetupBudget {
		t.Errorf("chat turn setup p95 %s exceeds %s", setupP95, chatSetupBudget)
	}
	if readP95 > chatSessionBudget {
		t.Errorf("chat read p95 %s exceeds %s", readP95, chatSessionBudget)
	}
}
