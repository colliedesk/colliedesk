package httpapi_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedConversations writes conversations with messages of about 2 KiB of incompressible text
// straight into the database, the way a large migrated workspace looks.
func seedConversations(t *testing.T, pool *pgxpool.Pool, workspace string, conversations, messages int) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(workspace_id,name,email) SELECT $1,'Customer '||n,'c'||n||'@seed.example.com' FROM generate_series(1,$2::int) n`, workspace, conversations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Ticket '||name FROM contacts WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT $1,c.id,'note',(SELECT string_agg(md5(c.id::text||n||k),'') FROM generate_series(1,64) k)
		FROM conversations c CROSS JOIN generate_series(1,$2::int) n WHERE c.workspace_id=$1`, workspace, messages); err != nil {
		t.Fatal(err)
	}
}

// blockingStorage parks every read of one workspace's files until released, so a test can hold an
// export inside its attachment phase.
type blockingStorage struct {
	attachments.Storage
	workspace string
	reached   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (b *blockingStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.HasPrefix(key, b.workspace+"/") {
		b.once.Do(func() { close(b.reached) })
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.Storage.Open(ctx, key)
}

type blockedExport struct {
	api            http.Handler
	pool           *pgxpool.Pool
	adminA, adminB string
	workspaceA     string
	storage        *blockingStorage
	firstDone      chan int
	releaseOnce    sync.Once
	// linkA and linkB were made before the export of workspace A started to hold its slot.
	linkA, linkB exportLink
}

func (b *blockedExport) unblock() { b.releaseOnce.Do(func() { close(b.storage.release) }) }

// startBlockedExport starts an export of workspace A that parks while reading its attachment.
func startBlockedExport(t *testing.T, mutate func(*httpapi.Config)) *blockedExport {
	t.Helper()
	disk, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedExport{storage: &blockingStorage{Storage: disk, reached: make(chan struct{}), release: make(chan struct{})}, firstDone: make(chan int, 1)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = blocked.storage
		if mutate != nil {
			mutate(config)
		}
	})
	blocked.api, blocked.pool = api, pool
	blocked.adminA = createSession(t, api, service, "Blocked Alpha", "admin@blocked-alpha.example.com")
	blocked.adminB = createSession(t, api, service, "Blocked Bravo", "admin@blocked-bravo.example.com")
	blocked.workspaceA = workspaceID(t, api, blocked.adminA)
	blocked.storage.workspace = blocked.workspaceA
	for _, token := range []string{blocked.adminA, blocked.adminB} {
		contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "C", "email": "c@seed.example.com"})
		conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "S"})
		message := create(t, api, token, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "n"})
		seedAttachment(t, pool, disk, token, api, message, "f.txt", "hello")
	}
	blocked.linkA, blocked.linkB = makeExportLink(t, api, blocked.adminA), makeExportLink(t, api, blocked.adminB)
	t.Cleanup(blocked.unblock)
	go func() {
		blocked.firstDone <- request(t, api, http.MethodGet, "/api/v1/export", nil, blocked.adminA, nil).Code
	}()
	select {
	case <-blocked.storage.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the first export never reached its attachment phase")
	}
	return blocked
}

func TestSecondExportOfTheSameWorkspaceIsRefusedDuringTheAttachmentPhase(t *testing.T) {
	blocked := startBlockedExport(t, nil)
	response := request(t, blocked.api, http.MethodGet, "/api/v1/export", nil, blocked.adminA, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409 while the first export is still sending files", response.Code, response.Body.String())
	}
	blocked.unblock()
	if code := <-blocked.firstDone; code != http.StatusOK {
		t.Fatalf("first export status=%d", code)
	}
	if got := request(t, blocked.api, http.MethodGet, "/api/v1/export", nil, blocked.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("export after the first finished: status=%d, want 200", got)
	}
}

func TestExportsBeyondTheServerLimitGet429WithRetryAfter(t *testing.T) {
	blocked := startBlockedExport(t, func(config *httpapi.Config) { config.MaxConcurrentExports = 1 })
	response := request(t, blocked.api, http.MethodGet, "/api/v1/export", nil, blocked.adminB, nil)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d Retry-After=%q body=%s, want 429 with Retry-After", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	blocked.unblock()
	<-blocked.firstDone
	if got := request(t, blocked.api, http.MethodGet, "/api/v1/export", nil, blocked.adminB, nil).Code; got != http.StatusOK {
		t.Fatalf("export after the slot freed: status=%d, want 200", got)
	}
}

// smallBuffers keeps the kernel from absorbing the archive, so a client that stops reading blocks
// the server's write almost at once.
func smallBuffers(server *httptest.Server) *http.Client {
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if tcp, ok := conn.(*net.TCPConn); ok && state == http.StateNew {
			_ = tcp.SetWriteBuffer(4096)
		}
	}
	dialer := &net.Dialer{}
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if tcp, ok := conn.(*net.TCPConn); ok && err == nil {
			_ = tcp.SetReadBuffer(4096)
		}
		return conn, err
	}}}
}

func TestClientThatStopsReadingReleasesItsConnectionsAndNeverBlocksOtherTenants(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.ExportWriteTimeout = 300 * time.Millisecond })
	stalled := createSession(t, api, service, "Stall Alpha", "admin@stall-alpha.example.com")
	other := createSession(t, api, service, "Stall Bravo", "admin@stall-bravo.example.com")
	seedConversations(t, pool, workspaceID(t, api, stalled), 400, 3)
	server := httptest.NewServer(api)
	defer server.Close()
	client := smallBuffers(server)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+stalled)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := response.Body.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	// The client now reads nothing more. Other tenants must still be served.
	mine, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/me", nil)
	mine.Header.Set("Authorization", "Bearer "+other)
	started := time.Now()
	served, err := http.DefaultClient.Do(mine)
	if err != nil || served.StatusCode != http.StatusOK || time.Since(started) > 2*time.Second {
		t.Fatalf("another tenant's request: %v status=%v after %v", err, served, time.Since(started))
	}
	served.Body.Close()
	// The write deadline ends the stalled export: every database connection comes back.
	deadline := time.Now().Add(5 * time.Second)
	for pool.Stat().AcquiredConns() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d connections still held by a stalled export", pool.Stat().AcquiredConns())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := request(t, api, http.MethodGet, "/api/v1/export", nil, stalled, nil).Code; got != http.StatusOK {
		t.Fatalf("export of the same workspace after the stall: status=%d, want 200 (lock released)", got)
	}
}

// idleSink sleeps inside the first callback, the way a slow client stalls the transaction.
type idleSink struct {
	sleep time.Duration
}

func (s idleSink) Workspace(domain.Workspace) error                                { return nil }
func (s idleSink) Agent(export.Agent) error                                        { time.Sleep(s.sleep); return nil }
func (s idleSink) Contact(export.Contact) error                                    { return nil }
func (s idleSink) ContactMerge(export.ContactMerge) error                          { return nil }
func (s idleSink) Tag(export.Tag) error                                            { return nil }
func (s idleSink) SavedReply(export.SavedReply) error                              { return nil }
func (s idleSink) SavedReplyVersion(export.SavedReplyVersion) error                { return nil }
func (s idleSink) SavedView(export.SavedView) error                                { return nil }
func (s idleSink) KBCollection(export.KBCollection) error                          { return nil }
func (s idleSink) KBArticle(export.KBArticle) error                                { return nil }
func (s idleSink) KBArticleVersion(export.KBArticleVersion) error                  { return nil }
func (s idleSink) KBArticleTranslation(export.KBArticleTranslation) error          { return nil }
func (s idleSink) KBImage(export.KBImage) error                                    { return nil }
func (s idleSink) KBMediaReplacement(export.KBMediaReplacement) error              { return nil }
func (s idleSink) KBVideo(export.KBVideo) error                                    { return nil }
func (s idleSink) HelpCenter(export.HelpCenter) error                              { return nil }
func (s idleSink) AgentRequest(export.AgentRequest) error                          { return nil }
func (s idleSink) ImportJob(export.ImportJob) error                                { return nil }
func (s idleSink) ImportPerson(export.ImportPerson) error                          { return nil }
func (s idleSink) AIProvider(export.AIProvider) error                              { return nil }
func (s idleSink) AISlot(export.AISlot) error                                      { return nil }
func (s idleSink) AISettingsEvent(export.AISettingsEvent) error                    { return nil }
func (s idleSink) Channel(export.Channel) error                                    { return nil }
func (s idleSink) ChannelEvent(export.ChannelEvent) error                          { return nil }
func (s idleSink) ChannelInboundEvent(export.ChannelInboundEvent) error            { return nil }
func (s idleSink) ChannelAILevel(export.ChannelAILevel) error                      { return nil }
func (s idleSink) ChannelThread(export.ChannelThread) error                        { return nil }
func (s idleSink) ChannelThreadItem(export.ChannelThreadItem) error                { return nil }
func (s idleSink) ChannelSender(export.ChannelSender) error                        { return nil }
func (s idleSink) NumberSeries(export.NumberSeries) error                          { return nil }
func (s idleSink) ConversationThreadItem(export.ConversationThreadItem) error      { return nil }
func (s idleSink) IntakeGathering(export.IntakeGathering) error                    { return nil }
func (s idleSink) IntakeDecision(export.IntakeDecision) error                      { return nil }
func (s idleSink) IntakeEvalRun(export.IntakeEvalRun) error                        { return nil }
func (s idleSink) MessageMove(export.MessageMove) error                            { return nil }
func (s idleSink) AISourceConsent(export.AISourceConsent) error                    { return nil }
func (s idleSink) AIUsage(export.AIUsage) error                                    { return nil }
func (s idleSink) AIDraft(export.AIDraft) error                                    { return nil }
func (s idleSink) AITriage(export.AITriage) error                                  { return nil }
func (s idleSink) AIChatSession(export.AIChatSession) error                        { return nil }
func (s idleSink) ResultsBaseline(export.ResultsBaseline) error                    { return nil }
func (s idleSink) AutofixInstallation(export.AutofixInstallation) error            { return nil }
func (s idleSink) AutofixRepo(export.AutofixRepo) error                            { return nil }
func (s idleSink) AutofixConnector(export.AutofixConnector) error                  { return nil }
func (s idleSink) Fix(export.Fix) error                                            { return nil }
func (s idleSink) FixEvent(export.FixEvent) error                                  { return nil }
func (s idleSink) BeginConversation(export.ConversationHead) error                 { return nil }
func (s idleSink) Message(export.Message) error                                    { return nil }
func (s idleSink) EndConversation([]domain.TimeEntry, []export.StatusChange) error { return nil }

func TestExportTransactionIsEndedByTheDatabaseWhenItIdlesTooLong(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Idle Alpha", "admin@idle-alpha.example.com")
	workspace := workspaceID(t, api, admin)
	create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "C", "email": "c@idle.example.com"})
	db := store.New(pool)
	limits := store.ExportLimits{IdleInTransaction: 300 * time.Millisecond}
	if err := db.ExportWorkspace(context.Background(), workspace, idleSink{sleep: 0}, limits); err != nil {
		t.Fatalf("a prompt export failed: %v", err)
	}
	err := db.ExportWorkspace(context.Background(), workspace, idleSink{sleep: 1500 * time.Millisecond}, limits)
	if err == nil {
		t.Fatal("a transaction idle for 1.5 s survived a 300 ms idle-in-transaction limit")
	}
	if err := db.ExportWorkspace(context.Background(), workspace, idleSink{sleep: 0}, limits); err != nil {
		t.Fatalf("export after the idle one was ended (lock must be gone): %v", err)
	}
}

func TestClientThatTricklesIsCutOffByTheTotalDeadline(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.ExportTimeout = 400 * time.Millisecond
	})
	slow := createSession(t, api, service, "Trickle Alpha", "admin@trickle-alpha.example.com")
	seedConversations(t, pool, workspaceID(t, api, slow), 400, 3)
	server := httptest.NewServer(api)
	defer server.Close()
	client := smallBuffers(server)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+slow)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	// One byte every 50 ms keeps every single write inside the 60 s write deadline.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		one := make([]byte, 1)
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				if _, err := response.Body.Read(one); err != nil {
					return
				}
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for pool.Stat().AcquiredConns() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d connections still held by an export past its total deadline", pool.Stat().AcquiredConns())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !exportSlotIsFree(t, api, slow) {
		t.Fatal("export after the deadline was refused: the slot was not freed")
	}
}

// exportSlotIsFree asks for an export and reports whether the server accepted it. A refusal
// (409 for this workspace, 429 for the server) is answered before any work starts. An accepted
// export may still outlive the short total deadline the test sets and then aborts its response
// with http.ErrAbortHandler; that panic is the handler's way to cut the connection, and it proves
// the slot was free, so the check must not depend on the export finishing in time.
func exportSlotIsFree(t *testing.T, api http.Handler, token string) (free bool) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered != http.ErrAbortHandler {
				panic(recovered)
			}
			free = true
		}
	}()
	code := request(t, api, http.MethodGet, "/api/v1/export", nil, token, nil).Code
	return code != http.StatusConflict && code != http.StatusTooManyRequests
}
