package zalo_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// call is one request the stand-in Zalo server got.
type call struct {
	Path, Token, SecretKey string
	Body                   map[string]any
	Form                   string
}

// fakeZalo stands in for Zalo's message API and token server; no test calls Zalo.
type fakeZalo struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []call
	// answers are the next answers of the message API, by order; empty answers success.
	answers []string
	// tokens is the access token the message API takes; another is refused with -216.
	tokens string
	ids    atomic.Int64
}

func newFakeZalo(t *testing.T) *fakeZalo {
	f := &fakeZalo{tokens: "access-1"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{Path: r.URL.Path, Token: r.Header.Get("access_token"), SecretKey: r.Header.Get("secret_key")}
		if r.URL.Path == "/v4/oa/access_token" || r.URL.Path == "/v2.0/oa/upload/image" {
			c.Form = string(body)
		} else {
			_ = json.Unmarshal(body, &c.Body)
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		var scripted string
		if len(f.answers) > 0 && r.URL.Path != "/v4/oa/access_token" {
			scripted, f.answers = f.answers[0], f.answers[1:]
		}
		current := f.tokens
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v4/oa/access_token":
			f.mu.Lock()
			f.tokens = "access-2"
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","expires_in":"90000"}`))
		case r.URL.Path == "/v2.0/oa/upload/image" && c.Token == current:
			_, _ = w.Write([]byte(`{"error":0,"data":{"attachment_id":"att-1"}}`))
		case scripted == "500":
			w.WriteHeader(http.StatusInternalServerError)
		case scripted != "":
			_, _ = w.Write([]byte(scripted))
		case c.Token != current:
			_, _ = w.Write([]byte(`{"error":-216,"message":"Access token is invalid"}`))
		default:
			_, _ = fmt.Fprintf(w, `{"error":0,"message":"Success","data":{"message_id":"sent-%d"}}`, f.ids.Add(1))
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeZalo) sends() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Path != "/v4/oa/access_token" {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeZalo) refreshes() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Path == "/v4/oa/access_token" {
			out = append(out, c)
		}
	}
	return out
}

// desk is a database with the Zalo type pointed at the stand-in.
type desk struct {
	pool    *pgxpool.Pool
	store   *store.Store
	zalo    *fakeZalo
	media   *httptest.Server
	client  *http.Client
	storage attachments.Storage
}

func newDesk(t *testing.T) desk {
	t.Helper()
	pool := databasetest.Pool(t)
	fake := newFakeZalo(t)
	registry, err := channels.NewRegistry(zalo.New(zalo.Config{MessageBase: fake.server.URL, OAuthBase: fake.server.URL, HTTP: fake.server.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := tokenseal.ParseKeys("k1:" + base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	media := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.TrimPrefix(r.URL.Path, "/")))
	}))
	t.Cleanup(media.Close)
	roots := x509.NewCertPool()
	roots.AddCert(media.Certificate())
	client := pipeline.FetchClient(map[string]bool{media.Listener.Addr().String(): true}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	return desk{pool: pool, store: store.New(pool).WithChannels(registry).WithObjects(storage).WithSealKeys(keys), zalo: fake, media: media, client: client, storage: storage}
}

var oaNumber atomic.Int64

func (d desk) workspace(t *testing.T, name, adminEmail string) (string, string) {
	t.Helper()
	ctx := context.Background()
	created, _, err := auth.New(d.pool, "https://desk.example.com").CreateWorkspace(ctx, name, adminEmail)
	if err != nil {
		t.Fatal(err)
	}
	var agentID string
	if err := d.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, created.ID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	return created.ID, agentID
}

// connect adds an OA with every secret, as an admin pastes them.
func (d desk) connect(t *testing.T, workspaceID string, config string) channels.Channel {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := d.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if config == "" {
		config = fmt.Sprintf(`{"oa_id":"%d","app_id":"700001"}`, 900000+oaNumber.Add(1))
	}
	channel, err := d.store.CreateChannel(ctx, workspaceID, zalo.Type, store.ChannelInput{Config: json.RawMessage(config)}, store.ChannelActor{AgentID: agentID})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{zalo.CredentialAppSecret: "app-secret-1", zalo.CredentialOASecretKey: "oa-secret-1",
		zalo.CredentialAccessToken: "access-1", zalo.CredentialRefreshToken: "refresh-1"} {
		if _, err := d.store.SetChannelCredential(ctx, workspaceID, channel.ID, name, value, store.ChannelActor{AgentID: agentID}); err != nil {
			t.Fatal(err)
		}
	}
	return channel
}

// event is a delivery as Zalo sends it, with neutral ids.
func event(name, oa, sender, recipient, id, text string, attachments ...map[string]any) channels.Raw {
	body := map[string]any{"app_id": "700001", "event_name": name, "timestamp": fmt.Sprint(time.Now().UnixMilli()),
		"sender": map[string]string{"id": sender}, "recipient": map[string]string{"id": recipient},
		"message": map[string]any{"msg_id": id, "text": text, "attachments": attachments}}
	if strings.Contains(name, "group") {
		body["oa_id"] = oa
	}
	encoded, _ := json.Marshal(body)
	return channels.Raw{ExternalEventID: id, JSON: encoded}
}

func TestZaloPassesTheChannelContract(t *testing.T) {
	d := newDesk(t)
	channeltest.Run(t, channeltest.Harness{Type: zalo.Type, Pool: d.pool, Store: d.store, Secret: "oa-secret-1", Workspace: d.workspace, FetchClient: d.client,
		Connect: func(t *testing.T, workspaceID string, _ int) channels.Channel { return d.connect(t, workspaceID, "") },
		Raw: func(t *testing.T, channel channels.Channel, spec channeltest.Spec) channels.Raw {
			if spec.File != "" {
				return event("user_send_file", channel.ExternalAccount, "1"+fmt.Sprint(len(spec.From))+spec.From, channel.ExternalAccount, spec.ID, spec.Text,
					map[string]any{"type": "file", "payload": map[string]any{"url": d.media.URL + "/" + spec.File, "name": "file.txt", "size": fmt.Sprint(len(spec.File))}})
			}
			return event("user_send_text", channel.ExternalAccount, "1"+fmt.Sprint(len(spec.From))+spec.From, channel.ExternalAccount, spec.ID, spec.Text)
		}})
}

// at is a desk with one workspace and one OA, whose groups open a conversation on "report bug".
type at struct {
	desk
	workspaceID, agentID string
	channel              channels.Channel
	journal              *pipeline.Pipeline
}

func newAt(t *testing.T) at {
	t.Helper()
	d := newDesk(t)
	workspaceID, agentID := d.workspace(t, "Zalo Desk", "admin@zalo-desk.example.com")
	channel := d.connect(t, workspaceID, fmt.Sprintf(`{"oa_id":"%d","app_id":"700001","staff_user_ids":["5550001"],"groups":{"g-1":"Tea Co"}}`, 900000+oaNumber.Add(1)))
	if _, err := d.store.UpdateChannel(context.Background(), workspaceID, channel.ID, store.ChannelInput{Settings: []byte(`{"commands":{"bug":{"match":"words","phrases":["report bug"]}}}`)},
		store.ChannelActor{AgentID: agentID}); err != nil {
		t.Fatal(err)
	}
	channel, err := d.store.GetChannel(context.Background(), workspaceID, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	return at{desk: d, workspaceID: workspaceID, agentID: agentID, channel: channel, journal: pipeline.New(d.store, nil).WithFetchClient(d.client)}
}

func (a at) receive(t *testing.T, raws ...channels.Raw) {
	t.Helper()
	if _, err := a.journal.Receive(context.Background(), a.channel, raws); err != nil {
		t.Fatal(err)
	}
	if _, err := a.journal.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	// What a group collects is stored once its quiet window passed.
	later := pipeline.New(a.store, func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }).WithFetchClient(a.client)
	if _, err := later.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (a at) one(t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	if err := a.pool.QueryRow(context.Background(), query, append([]any{a.workspaceID}, args...)...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

func (a at) reply(t *testing.T, conversationID, body string) string {
	t.Helper()
	message, err := a.store.CreateMessage(context.Background(), a.workspaceID, conversationID, store.Actor{AgentID: a.agentID}, "outgoing", body)
	if err != nil {
		t.Fatal(err)
	}
	return message.ID
}

func (a at) deliver(t *testing.T) { a.deliverAt(t, time.Minute) }

func (a at) deliverAt(t *testing.T, ahead time.Duration) {
	t.Helper()
	worker := outbox.New(a.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(ahead) }})
	if _, err := worker.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A reply goes where the conversation was said: into its group, or to the customer alone, quoting the
// message it answers. Taking messages in calls Zalo for nothing.
func TestAReplyGoesToTheGroupOrToTheCustomerQuotingTheirMessage(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "1110001", "g-1", "m-group", "report bug the scale prints nothing"),
		event("user_send_text", oa, "1110002", oa, "m-direct", "Hello, the invoice is wrong"))
	if calls := len(a.zalo.sends()) + len(a.zalo.refreshes()); calls != 0 {
		t.Fatalf("calls to Zalo while taking messages in = %d", calls)
	}
	group := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-group'`)
	direct := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-direct'`)
	if got := a.one(t, `SELECT COALESCE(ct.organization,'') FROM conversations c JOIN contacts ct ON ct.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`, group); got != "Tea Co" {
		t.Fatalf("the group's customer organization = %q", got)
	}
	a.reply(t, group, "Looking at the scale.")
	a.reply(t, direct, "Fixed the invoice.")
	a.deliver(t)
	sends := a.zalo.sends()
	if len(sends) != 2 {
		t.Fatalf("sends = %+v", sends)
	}
	byPath := map[string]call{}
	for _, c := range sends {
		byPath[c.Path] = c
	}
	groupCall, directCall := byPath["/v3.0/oa/group/message"], byPath["/v3.0/oa/message/cs"]
	if fmt.Sprint(groupCall.Body["recipient"]) != "map[group_id:g-1]" || groupCall.Token != "access-1" {
		t.Fatalf("group send = %+v", groupCall)
	}
	message, _ := directCall.Body["message"].(map[string]any)
	if fmt.Sprint(directCall.Body["recipient"]) != "map[user_id:1110002]" || message["quote_message_id"] != "m-direct" || message["text"] != "Fixed the invoice." {
		t.Fatalf("direct send = %+v", directCall)
	}
	if got := a.one(t, `SELECT count(*)::text FROM channel_outbox WHERE workspace_id=$1 AND status='sent'`); got != "2" {
		t.Fatalf("sent deliveries = %s", got)
	}
}

// A long reply goes in parts of 2000 characters, the quote on the first only; a quote Zalo refuses is
// sent again without it.
func TestALongReplyGoesInPartsAndARefusedQuoteIsDropped(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110002", oa, "m-1", "Hello"))
	conversation := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-1'`)
	a.zalo.answers = []string{`{"error":-201,"message":"quote not found"}`}
	a.reply(t, conversation, strings.Repeat("ạ", 1990)+" "+strings.Repeat("b", 100))
	a.deliver(t)
	sends := a.zalo.sends()
	if len(sends) != 3 {
		t.Fatalf("sends = %d, want the refused quote, then two parts", len(sends))
	}
	first, _ := sends[1].Body["message"].(map[string]any)
	second, _ := sends[2].Body["message"].(map[string]any)
	if _, quoted := first["quote_message_id"]; quoted || len([]rune(first["text"].(string))) != 1990 || second["text"] != strings.Repeat("b", 100) {
		t.Fatalf("parts = %v / %v", first, second)
	}
	if got := a.one(t, `SELECT count(*)::text FROM channel_sent_ids WHERE workspace_id=$1`); got != "2" {
		t.Fatalf("sent ids = %s, want one per part", got)
	}
}

// An access token Zalo refuses is refreshed once with the refresh token and the app's secret, both new
// tokens are kept, and the reply is sent with the new one.
func TestARefusedAccessTokenIsRefreshedOnceAndKept(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110002", oa, "m-1", "Hello"))
	conversation := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-1'`)
	a.zalo.mu.Lock()
	a.zalo.tokens = "rotated-elsewhere"
	a.zalo.mu.Unlock()
	a.reply(t, conversation, "On it.")
	a.deliver(t)
	refreshes := a.zalo.refreshes()
	if len(refreshes) != 1 || refreshes[0].SecretKey != "app-secret-1" || !strings.Contains(refreshes[0].Form, "refresh_token=refresh-1") ||
		!strings.Contains(refreshes[0].Form, "app_id=700001") {
		t.Fatalf("refreshes = %+v", refreshes)
	}
	sends := a.zalo.sends()
	if len(sends) != 2 || sends[1].Token != "access-2" {
		t.Fatalf("sends = %+v", sends)
	}
	for name, want := range map[string]string{zalo.CredentialAccessToken: "access-2", zalo.CredentialRefreshToken: "refresh-2"} {
		if value, _, _, err := a.store.OpenChannelCredential(context.Background(), a.workspaceID, a.channel.ID, name); err != nil || value != want {
			t.Fatalf("%s = %q (%v), want %q", name, value, err, want)
		}
	}
	if got := a.one(t, `SELECT status FROM channel_outbox WHERE workspace_id=$1`); got != "sent" {
		t.Fatalf("delivery = %s", got)
	}
}

// What the team writes (on the OA's own chat, or as one of its Zalo users in a group) is the team's, never
// a customer's request; a sticker is kept with a word for it.
func TestTheTeamsMessagesAreTheTeamsAndAStickerIsKept(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_group_text", oa, "5550001", "g-1", "s-1", "checking the printer"),
		event("oa_send_text", oa, oa, "1110002", "s-2", "We are on it"),
		event("user_send_sticker", oa, "1110002", oa, "k-1", ""))
	if got := a.one(t, `SELECT string_agg(external_id||'='||role,',' ORDER BY external_id) FROM channel_thread_items WHERE workspace_id=$1`); got != "k-1=customer,s-1=staff,s-2=staff" {
		t.Fatalf("items = %s", got)
	}
	if got := a.one(t, `SELECT string_agg(body,',') FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "[sticker]" {
		t.Fatalf("customer messages = %s", got)
	}
}

// A refused token and a quote Zalo cannot make in one reply: the token is refreshed, then the quote dropped.
func TestARefreshedReplyStillDropsAQuoteZaloCannotMake(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110002", oa, "m-1", "Hello"))
	conversation := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-1'`)
	a.zalo.answers = []string{`{"error":-216,"message":"Access token is invalid"}`, `{"error":-201,"message":"quote not found"}`}
	a.reply(t, conversation, "On it.")
	a.deliver(t)
	sends := a.zalo.sends()
	last, _ := sends[len(sends)-1].Body["message"].(map[string]any)
	if len(a.zalo.refreshes()) != 1 || len(sends) != 3 || last["quote_message_id"] != nil {
		t.Fatalf("refreshes %d, sends %+v", len(a.zalo.refreshes()), sends)
	}
	if got := a.one(t, `SELECT status FROM channel_outbox WHERE workspace_id=$1`); got != "sent" {
		t.Fatalf("delivery = %s", got)
	}
}

// A reply that went in part before Zalo failed sends the rest on its retry, and keeps the id of its first
// part as the id of what it sent.
func TestAResumedReplyKeepsItsFirstPartsID(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110002", oa, "m-1", "Hello"))
	conversation := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-1'`)
	a.zalo.answers = []string{`{"error":0,"message":"Success","data":{"message_id":"first-part"}}`, "500"}
	a.reply(t, conversation, strings.Repeat("a", 2500))
	a.deliver(t)
	if got := a.one(t, `SELECT status||':'||(meta->>'sent') FROM channel_outbox WHERE workspace_id=$1`); got != "queued:1" {
		t.Fatalf("after the second part failed: %s", got)
	}
	a.deliverAt(t, time.Hour)
	if got := a.one(t, `SELECT o.status||':'||o.external_id FROM channel_outbox o WHERE o.workspace_id=$1`); got != "sent:first-part" {
		t.Fatalf("after the retry: %s", got)
	}
}

func (a at) replyWith(t *testing.T, conversationID, body, name, contentType string, data []byte) string {
	t.Helper()
	object, err := a.storage.Put(context.Background(), a.workspaceID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	message, err := a.store.CreateMessage(context.Background(), a.workspaceID, conversationID, store.Actor{AgentID: a.agentID}, "outgoing", body,
		store.NewAttachment{Filename: name, ContentType: contentType, Object: object})
	if err != nil {
		t.Fatal(err)
	}
	return message.ID
}

// A picture of a reply goes up to Zalo and then as a message of its own, after the text; a file that is
// no picture, or a picture over 1 MB, is refused before anything goes, and says why.
func TestAReplyCarriesItsPicturesAndRefusesOtherFiles(t *testing.T) {
	a := newAt(t)
	oa := a.channel.ExternalAccount
	a.receive(t, event("user_send_text", oa, "1110002", oa, "m-1", "Hello"))
	conversation := a.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='zalo:m-1'`)
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("png-bytes")...)
	a.replyWith(t, conversation, "Here is where to click.", "where.png", "image/png", png)
	a.deliver(t)
	sends := a.zalo.sends()
	if len(sends) != 3 || sends[1].Path != "/v2.0/oa/upload/image" || !strings.Contains(sends[1].Form, `filename="where.png"`) || !strings.Contains(sends[1].Form, "png-bytes") {
		t.Fatalf("sends = %+v", sends)
	}
	picture, _ := sends[2].Body["message"].(map[string]any)
	if !strings.Contains(fmt.Sprint(picture), "attachment_id:att-1") || !strings.Contains(fmt.Sprint(picture), "media_type:image") {
		t.Fatalf("picture message = %v", picture)
	}
	if got := a.one(t, `SELECT status FROM channel_outbox WHERE workspace_id=$1`); got != "sent" {
		t.Fatalf("delivery = %s", got)
	}
	for _, c := range []struct {
		name, contentType, code string
		size                    int
	}{
		{"invoice.pdf", "application/pdf", zalo.CodeNoFiles, 10}, {"big.jpg", "image/jpeg", zalo.CodeFileTooLarge, zalo.MaxPicture + 1},
		{"moving.gif", "image/gif", zalo.CodeNoFiles, 10}, {"named.png", "image/png", zalo.CodeNoFiles, 10}} {
		before := len(a.zalo.sends())
		id := a.replyWith(t, conversation, "See the file.", c.name, c.contentType, make([]byte, c.size))
		a.deliver(t)
		if got := a.one(t, `SELECT status||':'||COALESCE(last_error_code,'') FROM channel_outbox WHERE workspace_id=$1 AND message_id=$2`, id); got != "failed:"+c.code {
			t.Fatalf("%s: %s", c.name, got)
		}
		if after := len(a.zalo.sends()); after != before {
			t.Fatalf("%s: %d calls to Zalo, want none", c.name, after-before)
		}
	}
}
