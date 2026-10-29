package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chatDesk is a workspace with its help center on, one published article, the AI chat feature on the
// server's provider (the recording fake) and a help chat channel.
type chatDesk struct {
	api, site                http.Handler
	pool                     *pgxpool.Pool
	fake                     *aiprovider.Fake
	admin, slug, channel, ws string
	article                  kbArticleView
	host, base, origin       string
}

func chatServerProvider() *ai.ServerProvider {
	return &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-key",
		Models: map[string][]string{ai.SlotChat: {"claude-haiku-5-5"}}, Prices: map[string]ai.Price{"claude-haiku-5-5": {Input: 0.1, Output: 0.5}}}
}

func newChatDesk(t *testing.T, name string, mutate func(*httpapi.Config)) chatDesk {
	t.Helper()
	fake := &aiprovider.Fake{FakeKind: aiprovider.KindAnthropic}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.AI = fakeAIConfig(fake)
		config.AI.Server = chatServerProvider()
		if mutate != nil {
			mutate(config)
		}
	})
	return newChatDeskOn(t, api, service, pool, fake, name)
}

func newChatDeskOn(t *testing.T, api http.Handler, service *auth.Service, pool *pgxpool.Pool, fake *aiprovider.Fake, name string) chatDesk {
	t.Helper()
	d := chatDesk{api: api, site: helpCenterSite(api), pool: pool, fake: fake}
	d.admin = createSession(t, api, service, name, "admin@"+strings.ToLower(strings.ReplaceAll(name, " ", "-"))+".example.com")
	d.slug, d.ws = workspaceSlug(t, api, d.admin), workspaceID(t, api, d.admin)
	enableHelpCenter(t, api, d.admin, name+" help", true)
	collection := kbCollection(t, api, d.admin, map[string]any{"name": "Orders"})
	d.article = kbArticle(t, api, d.admin, map[string]any{"collection_id": collection.ID, "title": "Shipping times", "status": "published",
		"body_md": "Orders ship in 2 days. Call us on +84 912 345 678."})
	if _, err := store.New(pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": true}, d.admin, http.StatusOK, nil)
	var created struct {
		ID string `json:"id"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "help_chat", "config": map[string]any{"greeting": "Hi!"}}, d.admin, http.StatusCreated, &created)
	d.channel = created.ID
	d.host, d.base, d.origin = testOriginHost, "/help/"+d.slug, testOrigin
	return d
}

// call sends a request to the chat as the help center's own page would: from its origin, with JSON.
func (d chatDesk) call(t *testing.T, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, d.base+path, body, "", nil)
	req.Host, req.RemoteAddr = d.host, "192.0.2.90:4000"
	if method != http.MethodGet {
		req.Header.Set("Origin", d.origin)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	return serve(d.site, req)
}

type chatEvent struct {
	Type           string `json:"type"`
	TurnID         string `json:"turn_id"`
	SessionCreated bool   `json:"session_created"`
	Text           string `json:"text"`
	Status         string `json:"status"`
	Code           string `json:"code"`
	Redacted       bool   `json:"redacted"`
	Sources        []struct {
		Number int64  `json:"number"`
		URL    string `json:"url"`
	} `json:"sources"`
}

func chatEvents(t *testing.T, response *httptest.ResponseRecorder) []chatEvent {
	t.Helper()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status=%d type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	var events []chatEvent
	lines := bufio.NewScanner(strings.NewReader(response.Body.String()))
	for lines.Scan() {
		var event chatEvent
		if err := json.Unmarshal(lines.Bytes(), &event); err != nil {
			t.Fatalf("line %q: %v", lines.Text(), err)
		}
		events = append(events, event)
	}
	return events
}

func chatCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", response.Header().Values("Set-Cookie"))
	}
	return cookies[0]
}

func chatReply(deltas ...string) aiprovider.FakeReply {
	usage := aiprovider.Usage{Reported: true, InputTokens: 40, OutputTokens: 30, CacheReadTokens: 900}
	return aiprovider.FakeReply{Deltas: deltas, Response: aiprovider.Response{Stop: aiprovider.StopDone, Model: "claude-haiku-5-5", Usage: usage,
		Hops: []aiprovider.Hop{{Model: "claude-haiku-5-5", Usage: usage}}}}
}

// A question streams back as NDJSON, sentence by sentence, with the article it cites; a phone number
// the article does not hold is hidden with its sentence. The browser's cookie keeps the chat: its turns
// come back, the next question carries the history, feedback is given and taken back, and New chat
// forgets it.
func TestAChatAnswersAsNDJSONAndRemembersTheBrowser(t *testing.T) {
	d := newChatDesk(t, "Chat Alpha", nil)
	number := d.article.Number
	d.fake.Script(chatReply("Orders ship in 2 days [#"+num(number)+"]. ", "Call +84 999 ", "888 777 now."),
		chatReply("Yes, within 2 days [#"+num(number)+"]."))
	first := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"})
	events := chatEvents(t, first)
	cookie := chatCookie(t, first)
	set := first.Header().Get("Set-Cookie")
	wantCookie := "__Secure-cd_chat_" + d.slug + "_" + d.channel[:8] + "="
	if !strings.HasPrefix(set, wantCookie) || !strings.Contains(set, "Path=/help/"+d.slug+";") || !strings.Contains(set, "Max-Age=2592000") ||
		!strings.Contains(set, "HttpOnly") || !strings.Contains(set, "Secure") || !strings.Contains(set, "SameSite=Lax") || strings.Contains(set, "Domain=") {
		t.Fatalf("Set-Cookie = %q", set)
	}
	if events[0].Type != "turn" || !events[0].SessionCreated || events[len(events)-1].Type != "done" || events[len(events)-1].Status != "answered" || !events[len(events)-1].Redacted {
		t.Fatalf("events = %+v", events)
	}
	var text strings.Builder
	var sources []chatEvent
	for _, event := range events {
		if event.Type == "delta" {
			text.WriteString(event.Text)
		}
		if event.Type == "sources" {
			sources = append(sources, event)
		}
	}
	if strings.Contains(text.String(), "999") || !strings.Contains(text.String(), ai.ChatHidden) || !strings.HasPrefix(text.String(), "Orders ship in 2 days") {
		t.Fatalf("streamed text = %q", text.String())
	}
	if len(sources) != 1 || len(sources[0].Sources) != 1 || sources[0].Sources[0].URL != testOrigin+"/help/"+d.slug+"/a/"+num(number) {
		t.Fatalf("sources = %+v", sources)
	}

	second := chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Really?"}, cookie))
	if second[0].SessionCreated {
		t.Fatal("the second question started another chat")
	}
	requests := d.fake.Requests()
	if last := requests[len(requests)-1]; len(last.Messages) != 3 || !strings.Contains(last.Messages[0].Text, "When does my order ship?") || strings.Contains(last.Messages[1].Text, "999") {
		t.Fatalf("history sent = %+v", last.Messages)
	}

	var session struct {
		Status string `json:"status"`
		Turns  []struct {
			ID       string  `json:"id"`
			Answer   string  `json:"answer"`
			Feedback *string `json:"feedback"`
			Redacted bool    `json:"redacted"`
		} `json:"turns"`
	}
	response := d.call(t, http.MethodGet, "/chat/session", nil, cookie)
	decode(t, response, &session)
	if session.Status != "open" || len(session.Turns) != 2 || !session.Turns[0].Redacted || strings.Contains(session.Turns[0].Answer, "999") {
		t.Fatalf("session = %+v", session)
	}
	turn := session.Turns[0].ID
	if response := d.call(t, http.MethodPut, "/chat/turns/"+turn+"/feedback", map[string]any{"rating": "not_helpful"}, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("feedback status=%d %s", response.Code, response.Body.String())
	}
	decode(t, d.call(t, http.MethodGet, "/chat/session", nil, cookie), &session)
	if session.Turns[0].Feedback == nil || *session.Turns[0].Feedback != "not_helpful" {
		t.Fatalf("feedback not kept: %+v", session.Turns[0])
	}
	if response := d.call(t, http.MethodDelete, "/chat/turns/"+turn+"/feedback", nil, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("feedback removal status=%d", response.Code)
	}
	var one struct {
		Status   string  `json:"status"`
		Feedback *string `json:"feedback"`
	}
	decode(t, d.call(t, http.MethodGet, "/chat/turns/"+turn, nil, cookie), &one)
	if one.Status != "answered" || one.Feedback != nil {
		t.Fatalf("turn = %+v", one)
	}

	ended := d.call(t, http.MethodPost, "/chat/session/end", nil, cookie)
	if ended.Code != http.StatusNoContent || !strings.Contains(ended.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("end status=%d cookie=%q", ended.Code, ended.Header().Get("Set-Cookie"))
	}
	if response := d.call(t, http.MethodGet, "/chat/session", nil, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("an ended chat came back: %d", response.Code)
	}
	// The team keeps its transcript.
	var chats chatPage
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai/chat/sessions", nil, d.admin, nil), &chats)
	if len(chats.Items) != 1 || chats.Items[0].Status != "ended" {
		t.Fatalf("team's chats = %+v", chats)
	}
}

// Writes come only from the help center's own pages, as JSON; a question has a length, an address a
// rate, a chat a size.
func TestTheChatRefusesWhatItShouldNot(t *testing.T) {
	d := newChatDesk(t, "Chat Bravo", nil)
	ask := map[string]any{"question": "Hi"}
	noOrigin := newRequest(t, http.MethodPost, d.base+"/chat/turns", ask, "", nil)
	if response := serve(d.site, noOrigin); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_rejected") {
		t.Fatalf("no Origin: %d %s", response.Code, response.Body.String())
	}
	foreign := d
	foreign.origin = "https://evil.example"
	wantResponse(t, foreign.call(t, http.MethodPost, "/chat/turns", ask), http.StatusForbidden, "csrf_rejected")
	plain := httptest.NewRequest(http.MethodPost, d.base+"/chat/turns", strings.NewReader(`{"question":"Hi"}`))
	plain.Host, plain.Header = testOriginHost, http.Header{"Origin": {testOrigin}, "Content-Type": {"text/plain"}}
	wantResponse(t, serve(d.site, plain), http.StatusUnsupportedMediaType, "unsupported_media_type")
	wantResponse(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": strings.Repeat("a", 1001)}), http.StatusUnprocessableEntity, "question_too_long")
	if len(d.fake.Requests()) != 0 {
		t.Fatal("a refused question reached the provider")
	}
	for i := 0; i < 9; i++ {
		chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi again"}))
	}
	limited := d.call(t, http.MethodPost, "/chat/turns", ask)
	wantResponse(t, limited, http.StatusTooManyRequests, "rate_limited")
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
}

// A chat that is off answers exactly as a help center that is not there: the AI chat feature off, the
// channel off, another workspace's channel, or no channel.
func TestAnOffChatIsTheHelpCentersOwn404(t *testing.T) {
	d := newChatDesk(t, "Chat Charlie", nil)
	missing := helpCenterCall(d.site, http.MethodGet, testOriginHost, "/help/no-such-help/chat/session")
	same := func(name string, response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != http.StatusNotFound || response.Body.String() != missing.Body.String() {
			t.Errorf("%s: status=%d, want the help center's own 404", name, response.Code)
		}
	}
	if response := d.call(t, http.MethodGet, "/chat/session", nil); response.Code != http.StatusNoContent {
		t.Fatalf("on: %d", response.Code)
	}
	other := newChatDeskOn(t, d.api, auth.New(d.pool, testOrigin), d.pool, d.fake, "Chat Other")
	same("another workspace's channel", d.call(t, http.MethodGet, "/chat/session?channel="+other.channel, nil))
	kbCall(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channel, map[string]any{"enabled": false}, d.admin, http.StatusOK, nil)
	same("channel off", d.call(t, http.MethodGet, "/chat/session", nil))
	same("channel off, a question", d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi"}))
	kbCall(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channel, map[string]any{"enabled": true}, d.admin, http.StatusOK, nil)
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": false}, d.admin, http.StatusOK, nil)
	same("AI chat off", d.call(t, http.MethodGet, "/chat/session", nil))
}

// A cookie of one workspace's chat means nothing at another's: a new chat starts, with no history.
func TestAChatCookieNeverCrossesWorkspaces(t *testing.T) {
	a := newChatDesk(t, "Chat Delta", nil)
	b := newChatDeskOn(t, a.api, auth.New(a.pool, testOrigin), a.pool, a.fake, "Chat Echo")
	a.fake.Script(chatReply("Orders ship in 2 days [#"+num(a.article.Number)+"]."), chatReply("Hello [#"+num(b.article.Number)+"]."))
	cookie := chatCookie(t, a.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "A's private question"}))
	// The same cookie value under B's cookie name.
	moved := &http.Cookie{Name: "__Secure-cd_chat_" + b.slug + "_" + b.channel[:8], Value: cookie.Value}
	if response := b.call(t, http.MethodGet, "/chat/session", nil, moved); response.Code != http.StatusNoContent {
		t.Fatalf("B read A's chat: %d %s", response.Code, response.Body.String())
	}
	events := chatEvents(t, b.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi"}, moved))
	if !events[0].SessionCreated {
		t.Fatal("B continued A's chat")
	}
	requests := a.fake.Requests()
	if last := requests[len(requests)-1]; len(last.Messages) != 1 || strings.Contains(last.Messages[0].Text, "A's private") {
		t.Fatalf("B's prompt holds A's history: %+v", last.Messages)
	}
	wantResponse(t, b.call(t, http.MethodGet, "/chat/turns/"+events[0].TurnID, nil, cookie), http.StatusNotFound, "not_found")
}

func num(n int64) string { return strconv.FormatInt(n, 10) }

// An end user who leaves mid-answer does not stop it: the answer is finished, stored and metered, and
// the browser reads it back.
func TestAChatAnswerIsKeptWhenTheEndUserLeaves(t *testing.T) {
	d := newChatDesk(t, "Chat Foxtrot", nil)
	hold := make(chan struct{})
	waiting := chatReply("Orders ship ", "in 2 days [#"+num(d.article.Number)+"].")
	waiting.Hold = hold
	d.fake.Script(waiting)
	ctx, cancel := context.WithCancel(context.Background())
	req := newRequest(t, http.MethodPost, d.base+"/chat/turns", map[string]any{"question": "When?"}, "", nil).WithContext(ctx)
	req.Host, req.RemoteAddr = d.host, "192.0.2.91:4000"
	req.Header.Set("Origin", d.origin)
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- serve(d.site, req) }()
	// The end user leaves while the answer is still being written.
	for len(d.fake.Requests()) == 0 {
		runtime.Gosched()
	}
	cancel()
	close(hold)
	response := <-done
	events := chatEvents(t, response)
	cookie := chatCookie(t, response)
	var turn struct {
		Status string `json:"status"`
		Answer string `json:"answer"`
	}
	decode(t, d.call(t, http.MethodGet, "/chat/turns/"+events[0].TurnID, nil, cookie), &turn)
	if turn.Status != "answered" || turn.Answer != "Orders ship in 2 days [#"+num(d.article.Number)+"]." {
		t.Fatalf("turn = %+v", turn)
	}
	var metered int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND feature='chat' AND status='ok' AND input_tokens=40`, d.ws).Scan(&metered); err != nil || metered != 1 {
		t.Fatalf("metered rows = %d %v", metered, err)
	}
}

// Past the chat's limit nothing is sent to the provider: the end user gets related articles and the
// turn is over_limit.
func TestAChatOverItsLimitOffersArticlesInsteadOfAnAnswer(t *testing.T) {
	d := newChatDesk(t, "Chat Golf", nil)
	if _, err := d.pool.Exec(context.Background(), `UPDATE ai_slots SET monthly_cap_micro_usd=1 WHERE workspace_id=$1 AND slot='chat'`, d.ws); err != nil {
		t.Fatal(err)
	}
	events := chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"}))
	last := events[len(events)-1]
	if last.Type != "done" || last.Status != "over_limit" || len(d.fake.Requests()) != 0 {
		t.Fatalf("events = %+v requests=%d", events, len(d.fake.Requests()))
	}
	var related []int64
	for _, event := range events {
		if event.Type == "sources" {
			for _, source := range event.Sources {
				related = append(related, source.Number)
			}
		}
	}
	if len(related) != 1 || related[0] != d.article.Number {
		t.Fatalf("related = %v", related)
	}
}

// The chat's cookie on a help center host of its own is __Host- with Path=/; in a site's frame it is
// partitioned (SameSite=None); a server on plain http sets neither prefix nor Secure.
func TestTheChatCookieFollowsTheAddress(t *testing.T) {
	d := newChatDesk(t, "Chat Hotel", func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	d.host, d.base, d.origin = d.slug+".help.example.net", "", "https://"+d.slug+".help.example.net"
	d.fake.Script(chatReply("Hi [#"+num(d.article.Number)+"]."), chatReply("Hi [#"+num(d.article.Number)+"]."))
	set := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi"}).Header().Get("Set-Cookie")
	if !strings.HasPrefix(set, "__Host-cd_chat_"+d.channel[:8]+"=") || !strings.Contains(set, "Path=/;") || !strings.Contains(set, "SameSite=Lax") {
		t.Fatalf("host cookie = %q", set)
	}
	widget := d.call(t, http.MethodPost, "/chat/turns?surface=widget", map[string]any{"question": "Hi"}).Header().Get("Set-Cookie")
	if !strings.HasPrefix(widget, "__Host-cd_chat_w_"+d.channel[:8]+"=") || !strings.Contains(widget, "SameSite=None") || !strings.Contains(widget, "Partitioned") {
		t.Fatalf("widget cookie = %q", widget)
	}

	plain := newChatDesk(t, "Chat India", func(config *httpapi.Config) { config.Origin = "http://desk.example.com" })
	plain.origin = "http://desk.example.com"
	plain.fake.Script(chatReply("Hi [#" + num(plain.article.Number) + "]."))
	set = plain.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi"}).Header().Get("Set-Cookie")
	if !strings.HasPrefix(set, "cd_chat_"+plain.slug+"_"+plain.channel[:8]+"=") || strings.Contains(set, "Secure") {
		t.Fatalf("plain http cookie = %q", set)
	}
}

// An admin tries the chat from Settings: the answer comes back whole, as an end user would see it,
// nothing is stored but the call's usage, and another workspace's channel is not found.
func TestAnAdminTriesTheChatWithoutStoringAChat(t *testing.T) {
	d := newChatDesk(t, "Chat Juliett", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#"+num(d.article.Number)+"]. ", "Call +84 999 888 777."))
	var preview struct {
		Answer   string `json:"answer"`
		Status   string `json:"status"`
		Redacted bool   `json:"redacted"`
		Whole    bool   `json:"whole"`
		Sources  []struct {
			Number int64 `json:"number"`
		} `json:"sources"`
	}
	decode(t, request(t, d.api, http.MethodPost, "/api/v1/ai/chat/preview", map[string]any{"question": "When?", "channel_id": d.channel}, d.admin, nil), &preview)
	if preview.Status != "answered" || !preview.Redacted || !preview.Whole || strings.Contains(preview.Answer, "999") || len(preview.Sources) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	var chats, usage int
	if err := d.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM ai_chat_sessions WHERE workspace_id=$1),
		(SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND feature='chat' AND actor_agent_id IS NOT NULL)`, d.ws).Scan(&chats, &usage); err != nil || chats != 0 || usage != 1 {
		t.Fatalf("chats=%d usage=%d %v", chats, usage, err)
	}
	other := newChatDeskOn(t, d.api, auth.New(d.pool, testOrigin), d.pool, d.fake, "Chat Kilo")
	wantResponse(t, request(t, d.api, http.MethodPost, "/api/v1/ai/chat/preview", map[string]any{"question": "When?", "channel_id": other.channel}, d.admin, nil),
		http.StatusNotFound, "not_found")
}

// The chat page and an article's chat box load the chat's own script, allowed by its full address in
// the Content-Security-Policy with Trusted Types; the page is framed only by the channel's sites with
// embed=1. With the chat off, articles carry no script and the strict policy, and the help center's
// pages change their ETag so none is served from before.
func TestTheChatPageAndTheArticleBoxRunOnlyWhileTheChatIsOn(t *testing.T) {
	d := newChatDesk(t, "Chat Lima", nil)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channel, map[string]any{"config": map[string]any{"widget_origins": []string{"https://shop.lima.example"}}}, d.admin, http.StatusOK, nil)
	script := testOrigin + d.base + "/chat/chat.js"
	page := d.call(t, http.MethodGet, "/chat", nil)
	csp := page.Header().Get("Content-Security-Policy")
	if page.Code != http.StatusOK || !strings.Contains(csp, "script-src "+script+";") || !strings.Contains(csp, "require-trusted-types-for 'script'") ||
		!strings.Contains(csp, "frame-ancestors 'none'") || page.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(page.Body.String(), `id="chat"`) ||
		!strings.Contains(page.Body.String(), `data-config="{`) || !strings.Contains(page.Body.String(), `surface&#34;:&#34;page`) {
		t.Fatalf("page status=%d csp=%q body=%.300s", page.Code, csp, page.Body.String())
	}
	embed := d.call(t, http.MethodGet, "/chat?embed=1", nil)
	if csp := embed.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors https://shop.lima.example;") || embed.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("embed csp=%q xfo=%q", csp, embed.Header().Get("X-Frame-Options"))
	}
	asset := d.call(t, http.MethodGet, "/chat/chat.js?v=x", nil)
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || asset.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("chat.js status=%d type=%q cache=%q", asset.Code, asset.Header().Get("Content-Type"), asset.Header().Get("Cache-Control"))
	}

	articlePath := d.base + "/a/" + num(d.article.Number) + "-" + d.article.Slug
	on := helpCenterCall(d.site, http.MethodGet, testOriginHost, articlePath)
	if on.Code != http.StatusOK || !strings.Contains(on.Body.String(), `<script src="`+d.base+`/chat/chat.js?v=`) || !strings.Contains(on.Body.String(), `class="chatbox"`) ||
		!strings.Contains(on.Header().Get("Content-Security-Policy"), "script-src "+script+";") {
		t.Fatalf("article with the chat on: status=%d csp=%q", on.Code, on.Header().Get("Content-Security-Policy"))
	}
	home := helpCenterCall(d.site, http.MethodGet, testOriginHost, d.base)
	if !strings.Contains(home.Body.String(), `class="ask-link" href="`+d.base+`/chat"`) || strings.Contains(home.Body.String(), "<script src") ||
		!strings.Contains(home.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatalf("home with the chat on: %.400s", home.Body.String())
	}

	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": false}, d.admin, http.StatusOK, nil)
	off := helpCenterCall(d.site, http.MethodGet, testOriginHost, articlePath)
	if off.Code != http.StatusOK || strings.Contains(off.Body.String(), "<script src") || strings.Contains(off.Body.String(), "ask-link") ||
		!strings.Contains(off.Header().Get("Content-Security-Policy"), "script-src 'none'") || off.Header().Get("ETag") == on.Header().Get("ETag") {
		t.Fatalf("article with the chat off: csp=%q etag on=%s off=%s", off.Header().Get("Content-Security-Policy"), on.Header().Get("ETag"), off.Header().Get("ETag"))
	}
}

// widget.js asks what to draw: a site the channel lists gets the name, greeting, suggestions and the
// brand's accent (null without a brand color), with CORS for that site only; any other site gets the
// help center's 404 without CORS, and the widget draws nothing.
func TestTheWidgetDrawsOnlyOnItsSites(t *testing.T) {
	d := newChatDesk(t, "Chat Mike", nil)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/channels/"+d.channel, map[string]any{"config": map[string]any{"widget_origins": []string{"https://Shop.Mike.example"},
		"suggestions": []string{"Where is my order?"}}}, d.admin, http.StatusOK, nil)
	ask := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, d.base+"/chat/widget-config", nil)
		req.Host = testOriginHost
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return serve(d.site, req)
	}
	var config struct {
		Name        string            `json:"name"`
		Suggestions []string          `json:"suggestions"`
		Brand       map[string]string `json:"brand"`
	}
	allowed := ask("https://shop.mike.example")
	decode(t, allowed, &config)
	if allowed.Header().Get("Access-Control-Allow-Origin") != "https://shop.mike.example" || config.Name != "Chat Mike" || len(config.Suggestions) != 1 || config.Brand != nil {
		t.Fatalf("allowed: %+v cors=%q", config, allowed.Header().Get("Access-Control-Allow-Origin"))
	}
	for _, origin := range []string{"https://evil.example", ""} {
		refused := ask(origin)
		if refused.Code != http.StatusNotFound || refused.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q: status=%d cors=%q", origin, refused.Code, refused.Header().Get("Access-Control-Allow-Origin"))
		}
	}
	// A brand color: the button takes the accent the brand sheet has.
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"colors": map[string]any{"primary": "#0b5cad"}, "custom_css": nil}, d.admin, http.StatusOK, nil)
	decode(t, ask("https://shop.mike.example"), &config)
	if config.Brand["accent"] != "#0b5cad" || config.Brand["accent_ink"] == "" {
		t.Fatalf("brand = %+v", config.Brand)
	}
}

// Open full chat moves a chat from a site's frame (its partitioned cookie) to the help center's own
// page once: the code works one time, then the page's own cookie reads the same chat.
func TestAChatMovesFromASitesFrameToTheChatPage(t *testing.T) {
	d := newChatDesk(t, "Chat November", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	framed := d.call(t, http.MethodPost, "/chat/turns?surface=widget", map[string]any{"question": "When?"})
	chatEvents(t, framed)
	frameCookie := chatCookie(t, framed)
	var moved struct {
		URL string `json:"url"`
	}
	decode(t, d.call(t, http.MethodPost, "/chat/transfer?surface=widget", nil, frameCookie), &moved)
	at := strings.Index(moved.URL, "&t=")
	if !strings.HasPrefix(moved.URL, testOrigin+d.base+"/chat?channel=") || at < 0 {
		t.Fatalf("url = %q", moved.URL)
	}
	path := strings.TrimPrefix(moved.URL, testOrigin+d.base)
	redeem := d.call(t, http.MethodGet, path, nil)
	if redeem.Code != http.StatusSeeOther || strings.Contains(redeem.Header().Get("Location"), "t=") {
		t.Fatalf("redeem status=%d location=%q", redeem.Code, redeem.Header().Get("Location"))
	}
	pageCookie := chatCookie(t, redeem)
	if pageCookie.Name != "__Secure-cd_chat_"+d.slug+"_"+d.channel[:8] || pageCookie.Value == frameCookie.Value {
		t.Fatalf("page cookie = %+v", pageCookie)
	}
	var session struct {
		Turns []struct {
			Question string `json:"question"`
		} `json:"turns"`
	}
	decode(t, d.call(t, http.MethodGet, "/chat/session", nil, pageCookie), &session)
	if len(session.Turns) != 1 || session.Turns[0].Question != "When?" {
		t.Fatalf("moved chat = %+v", session)
	}
	if again := d.call(t, http.MethodGet, path, nil); len(again.Result().Cookies()) != 0 {
		t.Fatal("a used code moved the chat again")
	}
	// The chat moved: the frame's cookie no longer reads it, so a link passed on shares nothing.
	if framed := d.call(t, http.MethodGet, "/chat/session?surface=widget", nil, frameCookie); framed.Code != http.StatusNoContent {
		t.Fatalf("frame after the move: status=%d", framed.Code)
	}
}

// A transfer code of workspace A moves nothing at workspace B (B's own help center, the same code): no
// cookie, and the code still works where it was made. A's chats list finds nothing by B's conversation.
func TestATransferCodeNeverCrossesWorkspaces(t *testing.T) {
	a := newChatDesk(t, "Chat Tango", nil)
	a.fake.Script(chatReply("Orders ship in 2 days [#" + num(a.article.Number) + "]."))
	framed := a.call(t, http.MethodPost, "/chat/turns?surface=widget", map[string]any{"question": "When?"})
	chatEvents(t, framed)
	var moved struct {
		URL string `json:"url"`
	}
	decode(t, a.call(t, http.MethodPost, "/chat/transfer?surface=widget", nil, chatCookie(t, framed)), &moved)
	code := moved.URL[strings.Index(moved.URL, "&t=")+3:]
	b := newChatDeskOn(t, a.api, auth.New(a.pool, testOrigin), a.pool, a.fake, "Chat Uniform")
	atB := b.call(t, http.MethodGet, "/chat?channel="+b.channel+"&t="+code, nil)
	if atB.Code != http.StatusSeeOther || len(atB.Result().Cookies()) != 0 {
		t.Fatalf("A's code at B: status=%d cookies=%v", atB.Code, atB.Result().Cookies())
	}
	if atA := a.call(t, http.MethodGet, "/chat?channel="+a.channel+"&t="+code, nil); len(atA.Result().Cookies()) != 1 {
		t.Fatalf("A's code at A after B: cookies=%v", atA.Result().Cookies())
	}

	b.fake.Script(chatReply("Orders ship in 2 days [#" + num(b.article.Number) + "]."))
	turn := b.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When?"})
	chatEvents(t, turn)
	if response := b.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "x@y.example"}, chatCookie(t, turn)); response.Code != http.StatusCreated {
		t.Fatalf("handoff at B: %d", response.Code)
	}
	var conversations struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	kbCall(t, b.api, http.MethodGet, "/api/v1/conversations", nil, b.admin, http.StatusOK, &conversations)
	var chats struct {
		Items []json.RawMessage `json:"items"`
	}
	kbCall(t, a.api, http.MethodGet, "/api/v1/ai/chat/sessions?conversation_id="+conversations.Items[0].ID, nil, a.admin, http.StatusOK, &chats)
	if len(chats.Items) != 0 {
		t.Fatalf("A's chats by B's conversation = %d", len(chats.Items))
	}
}

type handoffSent struct {
	Reference string `json:"reference"`
	Trial     bool   `json:"trial"`
}

// Talk to a person opens a conversation on the help chat channel from the typed address, with the note
// and the whole chat as text (questions, answers, the cited articles' links); a second hand-off of the
// same chat joins it. The chat's transcript links the conversation, and the conversation finds its chat.
func TestTalkToAPersonOpensOneConversationPerChat(t *testing.T) {
	d := newChatDesk(t, "Chat Oscar", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	first := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"})
	chatEvents(t, first)
	cookie := chatCookie(t, first)

	var sent handoffSent
	response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "Reader@Example.com", "name": " Ann  Reader ", "note": "Order 1182 please."}, cookie)
	if response.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, response, &sent)
	var conversations struct {
		Items []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
			Channel string `json:"channel"`
		} `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations", nil, d.admin, http.StatusOK, &conversations)
	if len(conversations.Items) != 1 || conversations.Items[0].Channel != "help_chat" || conversations.Items[0].Subject != "Help chat: When does my order ship?" ||
		!strings.HasPrefix(strings.ReplaceAll(strings.ToUpper(conversations.Items[0].ID), "-", ""), sent.Reference) || sent.Trial {
		t.Fatalf("conversations = %+v, sent = %+v", conversations.Items, sent)
	}
	conversation := conversations.Items[0].ID
	var timeline struct {
		Items []struct {
			Kind string `json:"kind"`
			Body string `json:"body"`
		} `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, d.admin, http.StatusOK, &timeline)
	body := timeline.Items[0].Body
	for _, want := range []string{"Order 1182 please.", "Customer: When does my order ship?", "AI: Orders ship in 2 days", "Source: Shipping times " + testOrigin + d.base + "/a/"} {
		if !strings.Contains(body, want) {
			t.Errorf("message body lacks %q:\n%s", want, body)
		}
	}

	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Can I pick it up?"}, cookie))
	again := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "reader@example.com"}, cookie)
	if again.Code != http.StatusCreated {
		t.Fatalf("second handoff status=%d", again.Code)
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations", nil, d.admin, http.StatusOK, &conversations)
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, d.admin, http.StatusOK, &timeline)
	if len(conversations.Items) != 1 || len(timeline.Items) != 2 {
		t.Fatalf("second hand-off: %d conversations, %d messages; want it joined", len(conversations.Items), len(timeline.Items))
	}
	// The second message holds only what was asked since the first: the conversation has the rest.
	if second := timeline.Items[1].Body; !strings.Contains(second, "Customer: Can I pick it up?") || strings.Contains(second, "When does my order ship?") {
		t.Fatalf("second hand-off message:\n%s", second)
	}
	var chats struct {
		Items []struct {
			Status         string  `json:"status"`
			ConversationID *string `json:"conversation_id"`
		} `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/ai/chat/sessions?conversation_id="+conversation, nil, d.admin, http.StatusOK, &chats)
	if len(chats.Items) != 1 || chats.Items[0].Status != "handed_off" || chats.Items[0].ConversationID == nil || *chats.Items[0].ConversationID != conversation {
		t.Fatalf("chat of the conversation = %+v", chats.Items)
	}
	// The chat goes on after a hand-off.
	d.fake.Script(chatReply("Yes [#" + num(d.article.Number) + "]."))
	if after := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Still there?"}, cookie); after.Code != http.StatusOK {
		t.Fatalf("turn after hand-off status=%d", after.Code)
	}
}

// A hand-off checks what was typed and answers like the rest of the chat: a bad address is 422 with the
// field, another site's page is refused, and a chat that is off is the help center's 404.
func TestTalkToAPersonRefusesWhatItShouldNot(t *testing.T) {
	d := newChatDesk(t, "Chat Papa", nil)
	for _, body := range []map[string]any{{"email": "not an address"}, {"email": "a@b.example", "note": strings.Repeat("x", 2001)}, {"email": "a@b.example", "name": strings.Repeat("y", 121)}} {
		if response := d.call(t, http.MethodPost, "/chat/handoff", body); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "validation_failed") {
			t.Errorf("body %v: status=%d %s", body, response.Code, response.Body.String())
		}
	}
	req := newRequest(t, http.MethodPost, d.base+"/chat/handoff", map[string]any{"email": "a@b.example"}, "", nil)
	req.Host = d.host
	req.Header.Set("Origin", "https://evil.example")
	if response := serve(d.site, req); response.Code != http.StatusForbidden {
		t.Errorf("another origin: status=%d", response.Code)
	}
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": false}, d.admin, http.StatusOK, nil)
	if response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "a@b.example"}); response.Code != http.StatusNotFound {
		t.Errorf("chat off: status=%d", response.Code)
	}
}

// A chat cookie of workspace A hands nothing of A's chat to workspace B: B's conversation carries no
// transcript of A, and A's chat stays open.
func TestAHandoffNeverCarriesAnotherWorkspacesChat(t *testing.T) {
	a := newChatDesk(t, "Chat Quebec", nil)
	a.fake.Script(chatReply("Orders ship in 2 days [#" + num(a.article.Number) + "]."))
	turn := a.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Secret question of A"})
	chatEvents(t, turn)
	cookie := chatCookie(t, turn)
	b := newChatDeskOn(t, a.api, auth.New(a.pool, testOrigin), a.pool, a.fake, "Chat Romeo")
	sameName := &http.Cookie{Name: strings.Replace(cookie.Name, a.slug+"_"+a.channel[:8], b.slug+"_"+b.channel[:8], 1), Value: cookie.Value}
	if response := b.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "x@y.example"}, cookie, sameName); response.Code != http.StatusCreated {
		t.Fatalf("handoff at B status=%d %s", response.Code, response.Body.String())
	}
	var conversations struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	kbCall(t, b.api, http.MethodGet, "/api/v1/conversations", nil, b.admin, http.StatusOK, &conversations)
	var timeline struct {
		Items []struct {
			Body string `json:"body"`
		} `json:"items"`
	}
	kbCall(t, b.api, http.MethodGet, "/api/v1/conversations/"+conversations.Items[0].ID+"/messages", nil, b.admin, http.StatusOK, &timeline)
	if strings.Contains(timeline.Items[0].Body, "Secret question of A") {
		t.Fatal("workspace B's conversation holds workspace A's chat")
	}
	var chats struct {
		Items []struct {
			Status string `json:"status"`
		} `json:"items"`
	}
	kbCall(t, a.api, http.MethodGet, "/api/v1/ai/chat/sessions", nil, a.admin, http.StatusOK, &chats)
	if len(chats.Items) != 1 || chats.Items[0].Status != "open" {
		t.Fatalf("A's chat = %+v", chats.Items)
	}
}

// On a Cloud trial, a reply to a help chat conversation is refused like one to a web form: anyone can
// type any address, so the chat must not turn the trial's mail into a way to write to it.
func TestTrialRepliesToHelpChatConversationsAreRefused(t *testing.T) {
	fake := &aiprovider.Fake{FakeKind: aiprovider.KindAnthropic}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.AI = fakeAIConfig(fake)
		config.AI.Server = chatServerProvider()
	})
	d := newChatDeskOn(t, f.api, f.billing.service, f.pool, fake, "Chat Sierra")
	connectEmail(t, f.api, d.admin, "support@chat-sierra.example.com")
	var sent handoffSent
	response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "anyone@victim.example"})
	decode(t, response, &sent)
	if response.Code != http.StatusCreated || !sent.Trial {
		t.Fatalf("handoff status=%d trial=%v", response.Code, sent.Trial)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, d.admin, nil), &page)
	wantStatus(t, f.reply(t, d.admin, page.Items[0].ID), http.StatusForbidden, restrictedCode, "trial reply to a help chat conversation")
}

// An answer that runs out of its time is stored as failed (timeout) with what was written, never left
// "streaming": the end user who reads it back is told, and the chat's turns count right.
func TestAChatAnswerOutOfTimeIsStoredAsFailed(t *testing.T) {
	d := newChatDesk(t, "Chat Victor", func(config *httpapi.Config) { config.AI.ChatAnswerTimeout = 300 * time.Millisecond })
	never := chatReply("Orders ship ", "in 2 days.")
	never.Hold = make(chan struct{})
	d.fake.Script(never)
	response := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When?"})
	events := chatEvents(t, response)
	var turn struct {
		Status    string `json:"status"`
		ErrorCode string `json:"error_code"`
	}
	decode(t, d.call(t, http.MethodGet, "/chat/turns/"+events[0].TurnID, nil, chatCookie(t, response)), &turn)
	if turn.Status != "failed" || turn.ErrorCode != "timeout" {
		t.Fatalf("turn = %+v, want failed/timeout", turn)
	}
}

// On Colliedesk Cloud the chat's limits are the plan's: a trial answers 5 questions a UTC day (50 a
// month); past them the end user gets the matching articles and nothing is sent to the AI. A paid plan
// has its own, larger limits, and the usage the admin reads says which.
func TestTheCloudPlanSetsTheChatsLimits(t *testing.T) {
	fake := &aiprovider.Fake{FakeKind: aiprovider.KindAnthropic}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.AI = fakeAIConfig(fake)
		config.AI.Server = chatServerProvider()
	})
	d := newChatDeskOn(t, f.api, f.billing.service, f.pool, fake, "Chat Whiskey")
	var usage struct {
		AnswerLimit    *int `json:"answer_limit"`
		DayAnswerLimit *int `json:"day_answer_limit"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/ai/chat/usage", nil, d.admin, http.StatusOK, &usage)
	if usage.AnswerLimit == nil || *usage.AnswerLimit != 50 || usage.DayAnswerLimit == nil || *usage.DayAnswerLimit != 5 {
		t.Fatalf("trial usage limits = %v/%v, want 50/5", usage.AnswerLimit, usage.DayAnswerLimit)
	}
	// Five answers today already.
	if _, err := f.pool.Exec(context.Background(), `WITH s AS (INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface,created_at,last_turn_at)
			VALUES($1,$2,sha256('earlier'::bytea),'help_center',$3,$3) RETURNING id)
		INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,answer,status,created_at,finished_at)
		SELECT $1,s.id,n,'Earlier?','Yes.','answered',$3,$3 FROM s, generate_series(1,5) n`, d.ws, d.channel, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	requests := len(fake.Requests())
	events := chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"}))
	last := events[len(events)-1]
	if last.Type != "done" || last.Status != "over_limit" || len(fake.Requests()) != requests {
		t.Fatalf("trial past its day: last=%+v, provider requests %d → %d", last, requests, len(fake.Requests()))
	}
	sources := 0
	for _, event := range events {
		if event.Type == "sources" {
			sources = len(event.Sources)
		}
	}
	if sources == 0 {
		t.Fatal("a paused chat offered no article")
	}

	f.billing.grantComp(t, d.ws, nil)
	kbCall(t, d.api, http.MethodGet, "/api/v1/ai/chat/usage", nil, d.admin, http.StatusOK, &usage)
	if usage.AnswerLimit == nil || *usage.AnswerLimit != 1500 {
		t.Fatalf("team usage limit = %v, want 1500", usage.AnswerLimit)
	}
	fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	events = chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"}))
	if last := events[len(events)-1]; last.Status != "answered" {
		t.Fatalf("team plan: last=%+v, want answered", last)
	}
}

// The export carries every chat with its turns, sources and feedback, in pages: 250 chats (more than
// one page) come out whole, oldest first, and only this workspace's.
func TestTheExportCarriesEveryChatInPages(t *testing.T) {
	d := newChatDesk(t, "Chat Xray", nil)
	other := newChatDeskOn(t, d.api, auth.New(d.pool, testOrigin), d.pool, d.fake, "Chat Yankee")
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `WITH s AS (INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface,created_at,last_turn_at)
			SELECT $1,$2,sha256(n::text::bytea),'help_center',now()-make_interval(mins=>300-n),now() FROM generate_series(1,250) n RETURNING id),
		t AS (INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,answer,status,created_at)
			SELECT $1,s.id,k,'Q'||k,'A [#1]','answered',now() FROM s, generate_series(1,2) k RETURNING id)
		INSERT INTO ai_chat_sources(workspace_id,turn_id,ordinal,article_number,article_version,title,folder) SELECT $1,t.id,1,1,1,'Shipping','Orders' FROM t`,
		d.ws, d.channel); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface,created_at,last_turn_at) VALUES($1,$2,sha256('b'::bytea),'help_center',now(),now())`,
		other.ws, other.channel); err != nil {
		t.Fatal(err)
	}
	response := request(t, d.api, http.MethodGet, "/api/v1/export", nil, d.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	chats := ndjsonLines(t, unzip(t, response.Body.Bytes())["ai-chat-sessions.ndjson"])
	if len(chats) != 250 {
		t.Fatalf("exported %d chats, want 250", len(chats))
	}
	previous := ""
	for _, chat := range chats {
		turns, _ := chat["turns"].([]any)
		if len(turns) != 2 {
			t.Fatalf("chat %v has %d turns", chat["id"], len(turns))
		}
		sources, _ := turns[1].(map[string]any)["sources"].([]any)
		if len(sources) != 1 {
			t.Fatalf("turn has %d sources", len(sources))
		}
		created, _ := chat["created_at"].(string)
		if created < previous {
			t.Fatalf("chats out of order: %s after %s", created, previous)
		}
		previous = created
	}
}

// A server whose operator removes its AI provider (or the chat's model) after a workspace turned the
// chat on still serves the help center: its pages answer without Ask AI or the article's chat box, every
// chat address is the help center's own 404 (never a 500), and the channel says why the chat is not
// showing.
func TestTheHelpCenterRunsWithoutTheChatWhenTheServerOffersNoModel(t *testing.T) {
	var server *ai.ServerProvider
	d := newChatDesk(t, "Chat Zulu", func(config *httpapi.Config) { server = config.AI.Server })
	// Another workspace's chat that is off says so, and does not turn this one's status.
	other := newChatDeskOn(t, d.api, auth.New(d.pool, testOrigin), d.pool, d.fake, "Chat Yankee")
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": false}, other.admin, http.StatusOK, nil)
	if state, reason := chatChannelStatus(t, other); state != "needs_attention" || reason != "ai_chat_off" {
		t.Fatalf("the other workspace's chat that is off: %s %s", state, reason)
	}
	if state, _ := chatChannelStatus(t, d); state == "needs_attention" {
		t.Fatalf("a chat that is on reads another workspace's: %s", state)
	}
	server.Models = nil
	missing := helpCenterCall(d.site, http.MethodGet, testOriginHost, "/help/no-such-help/chat/session")
	for _, path := range []string{"/chat", "/chat/session", "/chat/widget.js", "/chat/widget-config"} {
		if response := d.call(t, http.MethodGet, path, nil); response.Code != http.StatusNotFound || response.Body.String() != missing.Body.String() {
			t.Errorf("%s: status=%d, want the help center's own 404", path, response.Code)
		}
	}
	if response := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Hi"}); response.Code != http.StatusNotFound {
		t.Errorf("a question: status=%d, want 404", response.Code)
	}
	home := helpCenterCall(d.site, http.MethodGet, testOriginHost, d.base)
	article := helpCenterCall(d.site, http.MethodGet, testOriginHost, d.base+"/a/"+num(d.article.Number)+"-"+d.article.Slug)
	for name, page := range map[string]*httptest.ResponseRecorder{"home": home, "article": article} {
		if page.Code != http.StatusOK || strings.Contains(page.Body.String(), "ask-link") || strings.Contains(page.Body.String(), "chatbox") || strings.Contains(page.Body.String(), "<script src") {
			t.Errorf("%s: status=%d, want the page without the chat: %.300s", name, page.Code, page.Body.String())
		}
	}
	if state, reason := chatChannelStatus(t, d); state != "needs_attention" || reason != "ai_chat_off" {
		t.Fatalf("channel status = %s %s, want needs_attention ai_chat_off", state, reason)
	}
	if len(d.fake.Requests()) != 0 {
		t.Fatalf("the provider was called %d times", len(d.fake.Requests()))
	}
}

func chatChannelStatus(t *testing.T, d chatDesk) (state, reason string) {
	t.Helper()
	var channel struct {
		Status struct {
			State      string `json:"state"`
			ReasonCode string `json:"reason_code"`
		} `json:"status"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channel, nil, d.admin, http.StatusOK, &channel)
	return channel.Status.State, channel.Status.ReasonCode
}
