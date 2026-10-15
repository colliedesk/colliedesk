package retention_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/retention"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func at(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

type sentMail struct{ To, Raw string }

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []sentMail
	fail func(to string) error
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		if err := m.fail(to[0]); err != nil {
			return err
		}
	}
	m.sent = append(m.sent, sentMail{To: to[0], Raw: string(message)})
	return nil
}

func (m *recordingMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// fakePolar stands in for the Polar API, an external boundary.
type fakePolar struct {
	paid map[string]bool
	err  error
	asks []string
}

func (f *fakePolar) HasPaidSubscription(_ context.Context, externalID string, _ time.Time) (bool, error) {
	f.asks = append(f.asks, externalID)
	return f.paid[externalID], f.err
}

type workspace struct {
	ID, Slug string
	Keys     []string
}

type env struct {
	pool    *pgxpool.Pool
	db      *database.DB
	store   *store.Store
	disk    *attachments.LocalDisk
	polar   *fakePolar
	root    string
	mailer  *recordingMailer
	now     time.Time
	service *retention.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL retention test")
	}
	schema := "test_retention_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	local, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{pool: pool, db: database.New(pool), store: store.New(pool), disk: local, root: root, mailer: &recordingMailer{}, polar: &fakePolar{paid: map[string]bool{}}}
	e.service = e.serviceWith(nil)
	return e
}

// serviceWith builds a service with deletion on and a cap of 5 a day, then lets change adjust it.
func (e *env) serviceWith(change func(*retention.Config)) *retention.Service {
	config := retention.Config{DB: e.db, Storage: e.disk, Mailer: e.mailer, From: "noreply@mail.colliedesk.example",
		Origin: "https://app.colliedesk.example", Now: func() time.Time { return e.now },
		DeleteEnabled: true, MaxDeletionsPerDay: 5, Polar: e.polar}
	if change != nil {
		change(&config)
	}
	return retention.New(config)
}

// workspace creates a workspace with the admins given, one conversation with an attachment, and a
// trial that ended at trialEnd.
func (e *env) workspace(t *testing.T, name string, trialEnd string, admins ...string) workspace {
	t.Helper()
	ctx := context.Background()
	created, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, name, admins[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range admins[1:] {
		e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'admin')`, created.ID, extra)
	}
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'agent')`, created.ID, "agent-"+created.Slug+"@example.com")
	if _, err := e.store.SetTrialEnd(ctx, created.ID, at(trialEnd), "test", at("2026-10-15T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	contact, err := e.store.CreateContact(ctx, created.ID, "Customer", "customer-"+created.Slug+"@example.org")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := e.store.CreateConversation(ctx, created.ID, contact.ID, "Where is my order")
	if err != nil {
		t.Fatal(err)
	}
	result := workspace{ID: created.ID, Slug: created.Slug}
	var agentID string
	err = e.db.Privileged(ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1 AND email=$2`, created.ID, admins[0]).Scan(&agentID)
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		message, err := e.store.CreateMessage(ctx, created.ID, conversation.ID, agentID, "outgoing", "hello")
		if err != nil {
			t.Fatal(err)
		}
		object, err := e.disk.Put(ctx, created.ID, bytes.NewReader([]byte(fmt.Sprintf("file %s %d", name, index))), -1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.CreateAttachment(ctx, created.ID, store.NewAttachment{MessageID: message.ID, Filename: "f.txt", Object: object}); err != nil {
			t.Fatal(err)
		}
		result.Keys = append(result.Keys, object.Key)
	}
	return result
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) exists(t *testing.T, id string) bool {
	t.Helper()
	var found bool
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, id).Scan(&found)
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// rows counts the rows of every table that has a workspace_id column.
func (e *env) rows(t *testing.T, id string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		ctx := context.Background()
		rows, err := tx.Query(ctx, `SELECT table_name FROM information_schema.columns WHERE table_schema=current_schema() AND column_name='workspace_id' ORDER BY 1`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, table := range tables {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()+` WHERE workspace_id=$1`, id).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				counts[table] = n
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func (e *env) blobExists(key string) bool {
	_, err := os.Stat(filepath.Join(e.root, filepath.FromSlash(key)))
	return err == nil
}

func (e *env) noticeSentAt(t *testing.T, id string) *time.Time {
	t.Helper()
	var sent *time.Time
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT retention_notice_sent_at FROM workspaces WHERE id=$1`, id).Scan(&sent)
	})
	if err != nil {
		t.Fatal(err)
	}
	return sent
}

func (e *env) run(t *testing.T, now string) retention.Result {
	t.Helper()
	e.now = at(now)
	result, err := e.service.Run(context.Background())
	if err != nil {
		t.Fatalf("run at %s: %v", now, err)
	}
	return result
}

func (e *env) pay(t *testing.T, id, subscription, status string, periodEnd *time.Time) {
	t.Helper()
	_, err := e.store.ApplySubscription(context.Background(), id, "hook-"+subscription+status, "subscription.updated", domain.Subscription{
		Plan: domain.PlanTeam, SubscriptionID: subscription, CustomerID: "cus_1", Status: status, CurrentPeriodEnd: periodEnd, ModifiedAt: at("2027-10-15T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoticeThirtyDaysBeforeThenDeletionAfterTwelveMonths(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example", "bob@alpha.example")
	b := e.workspace(t, "Beta", "2027-03-01T00:00:00Z", "carl@beta.example")
	beforeB := e.rows(t, b.ID)

	if got := e.run(t, "2027-10-01T23:59:59Z"); got.Noticed != 0 || got.Deleted != 0 || e.mailer.count() != 0 {
		t.Fatalf("one second before the notice date: %+v, mails %d; want nothing", got, e.mailer.count())
	}
	if got := e.run(t, "2027-10-02T00:00:00Z"); got.Noticed != 1 || got.Deleted != 0 {
		t.Fatalf("notice day: %+v, want 1 noticed", got)
	}
	if e.mailer.count() != 2 {
		t.Fatalf("mails = %d, want one per admin (the plain agent gets none)", e.mailer.count())
	}
	first := e.mailer.sent[0]
	message, err := mail.ReadMessage(strings.NewReader(first.Raw))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(message.Body))
	if message.Header.Get("Subject") != "Your Colliedesk workspace Alpha will be deleted on 2027-11-01" || !strings.Contains(message.Header.Get("From"), "<noreply@mail.colliedesk.example>") {
		t.Errorf("headers: subject %q from %q", message.Header.Get("Subject"), message.Header.Get("From"))
	}
	for _, want := range []string{"deleted on 2027-11-01 (UTC)", "https://app.colliedesk.example", "export all your data from Colliedesk (Settings, or GET /api/v1/export) before 2027-11-01"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("notice lacks %q:\n%s", want, body)
		}
	}
	if sent := e.noticeSentAt(t, a.ID); sent == nil || !sent.Equal(at("2027-10-02T00:00:00Z")) {
		t.Fatalf("notice_sent_at = %v, want 2027-10-02T00:00:00Z", sent)
	}
	if !e.exists(t, a.ID) {
		t.Fatal("workspace deleted on the notice day")
	}

	if got := e.run(t, "2027-10-20T00:00:00Z"); got.Noticed != 0 || e.mailer.count() != 2 {
		t.Fatalf("second run sent again: %+v, mails %d", got, e.mailer.count())
	}
	if got := e.run(t, "2027-10-31T23:59:59Z"); got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("deleted a second early: %+v", got)
	}
	got := e.run(t, "2027-11-01T00:00:00Z")
	if got.Deleted != 1 {
		t.Fatalf("deletion day: %+v, want 1 deleted", got)
	}
	if e.exists(t, a.ID) || len(e.rows(t, a.ID)) != 0 {
		t.Fatalf("workspace A left rows behind: %v", e.rows(t, a.ID))
	}
	for _, key := range a.Keys {
		if e.blobExists(key) {
			t.Fatalf("blob %s still on disk", key)
		}
	}
	if _, err := os.Stat(filepath.Join(e.root, a.ID)); !os.IsNotExist(err) {
		t.Fatalf("workspace directory still exists: %v", err)
	}
	if !e.exists(t, b.ID) || fmt.Sprint(e.rows(t, b.ID)) != fmt.Sprint(beforeB) || !e.blobExists(b.Keys[0]) || !e.blobExists(b.Keys[1]) {
		t.Fatal("workspace B was touched")
	}
	again := e.run(t, "2027-11-02T00:00:00Z")
	if again != (retention.Result{}) {
		t.Fatalf("second run after deletion: %+v, want a no-op", again)
	}
}

func TestDeletionWaitsThirtyDaysAfterALateNotice(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-11-05T00:00:00Z") // the job was down; the notice goes out now
	if e.mailer.count() != 1 {
		t.Fatalf("mails = %d, want 1", e.mailer.count())
	}
	if !strings.Contains(e.mailer.sent[0].Raw, "2027-12-05") {
		t.Fatalf("notice must state the real deletion date 2027-12-05:\n%s", e.mailer.sent[0].Raw)
	}
	if got := e.run(t, "2027-12-04T23:59:59Z"); got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("deleted before the notice was 30 days old: %+v", got)
	}
	if got := e.run(t, "2027-12-05T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("not deleted 30 days after the notice: %+v", got)
	}
}

func TestPayingCancelsDeletionAndResetsTheNotice(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	e.pay(t, a.ID, "sub_1", "active", nil)
	if got := e.run(t, "2027-10-15T00:00:00Z"); got.Reset != 1 || e.noticeSentAt(t, a.ID) != nil {
		t.Fatalf("paying must reset the notice: %+v sent %v", got, e.noticeSentAt(t, a.ID))
	}
	if got := e.run(t, "2027-11-01T00:00:00Z"); got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("deleted a paying workspace: %+v", got)
	}
	// The subscription ends 2028-01-10: a new lock, a new twelve months, a new notice.
	e.exec(t, `UPDATE workspace_billing SET status='canceled', ended_at=$2 WHERE workspace_id=$1`, a.ID, at("2028-01-10T00:00:00Z"))
	if got := e.run(t, "2028-12-10T00:00:00Z"); got.Noticed != 0 || got.Deleted != 0 {
		t.Fatalf("a month early: %+v", got)
	}
	if got := e.run(t, "2028-12-11T00:00:00Z"); got.Noticed != 1 || e.mailer.count() != 2 {
		t.Fatalf("second lock must get its own notice: %+v mails %d", got, e.mailer.count())
	}
	if got := e.run(t, "2029-01-10T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("second lock not deleted twelve months after it began: %+v", got)
	}
}

func TestOldNoticeFromAnEarlierLockDoesNotCount(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	// Paid from 2027-10-10, lapsed 2028-01-10; the job never ran while the workspace had access.
	e.pay(t, a.ID, "sub_1", "canceled", nil)
	e.exec(t, `UPDATE workspace_billing SET ended_at=$2 WHERE workspace_id=$1`, a.ID, at("2028-01-10T00:00:00Z"))
	if got := e.run(t, "2028-01-11T00:00:00Z"); got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("a notice from the first lock authorised a deletion in the second: %+v", got)
	}
	if e.noticeSentAt(t, a.ID) != nil {
		t.Fatal("stale notice must be cleared while the new lock is young")
	}
}

func TestComplimentaryGrantKeepsTheWorkspaceAndRevokingStartsALock(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	if _, err := e.store.GrantCompPlan(context.Background(), a.ID, domain.PlanTeam, "ops", nil, at("2027-01-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := e.run(t, "2028-06-01T00:00:00Z"); got != (retention.Result{}) || e.mailer.count() != 0 {
		t.Fatalf("complimentary workspace was noticed or deleted: %+v", got)
	}
	if err := e.store.RevokeCompPlan(context.Background(), a.ID, "ops", at("2028-06-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := e.run(t, "2029-05-02T00:00:00Z"); got.Noticed != 1 {
		t.Fatalf("notice is due 30 days before 2029-06-01, on 2029-05-02: %+v", got)
	}
	if got := e.run(t, "2029-06-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("revoked grant: deletion 12 months after the revoke date: %+v", got)
	}
}

func (e *env) failDeletes(t *testing.T, deferred bool) {
	t.Helper()
	timing := ""
	if deferred {
		timing = "DEFERRABLE INITIALLY DEFERRED"
	}
	e.exec(t, `CREATE FUNCTION refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'delete refused'; END $$`)
	e.exec(t, `CREATE CONSTRAINT TRIGGER refuse_delete AFTER DELETE ON workspaces `+timing+` FOR EACH ROW EXECUTE FUNCTION refuse_delete()`)
}

func TestFailedDatabaseDeleteBringsTheFilesBack(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprint("at commit: ", deferred), func(t *testing.T) {
			e := newEnv(t)
			a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
			e.run(t, "2027-10-02T00:00:00Z")
			e.failDeletes(t, deferred)
			e.now = at("2027-11-01T00:00:00Z")
			if _, err := e.service.Run(context.Background()); err == nil {
				t.Fatal("run must report the failed delete")
			}
			if !e.exists(t, a.ID) || len(e.rows(t, a.ID)) == 0 {
				t.Fatal("the workspace row must stay")
			}
			for _, key := range a.Keys {
				if !e.blobExists(key) {
					t.Fatalf("blob %s was lost although the row stayed", key)
				}
			}
			// A customer who pays now still has every file.
			e.pay(t, a.ID, "sub_1", "active", nil)
			e.exec(t, `DROP TRIGGER refuse_delete ON workspaces`)
			if got := e.run(t, "2027-11-01T01:00:00Z"); got.Deleted != 0 || got.Reset != 1 || !e.blobExists(a.Keys[0]) {
				t.Fatalf("after paying: %+v", got)
			}
		})
	}
}

func TestDeletedFilesWaitThirtyDaysInTheTrash(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	e.run(t, "2027-11-01T00:00:00Z")
	trashed := filepath.Join(e.root, ".trash", a.ID+"-1825027200") // 2027-11-01T00:00:00Z
	if _, err := os.Stat(trashed); err != nil {
		t.Fatalf("files not in the trash: %v", err)
	}
	for _, key := range a.Keys {
		if e.blobExists(key) {
			t.Fatalf("%s still served from its old place", key)
		}
	}
	e.run(t, "2027-12-01T00:00:00Z") // 30 days: still kept
	if _, err := os.Stat(trashed); err != nil {
		t.Fatalf("trash purged after exactly 30 days: %v", err)
	}
	e.run(t, "2027-12-01T00:00:01Z")
	if _, err := os.Stat(trashed); !os.IsNotExist(err) {
		t.Fatalf("trash survived 30 days: %v", err)
	}
}

func TestBackdatedTrialEndGivesAFullTwelveMonthsFromTheChange(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	// An operator ends the trial "in 2020" on 2027-01-01.
	if _, err := e.store.SetTrialEnd(context.Background(), a.ID, at("2020-01-01T00:00:00Z"), "ops", at("2027-01-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if got := e.run(t, "2027-02-01T00:00:00Z"); got != (retention.Result{}) || e.mailer.count() != 0 {
		t.Fatalf("a backdated trial end made the workspace due: %+v", got)
	}
	if got := e.run(t, "2027-12-02T00:00:00Z"); got.Noticed != 1 {
		t.Fatalf("notice is due 30 days before 2028-01-01: %+v", got)
	}
	if got := e.run(t, "2028-01-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("deletion twelve months after the change: %+v", got)
	}
}

func TestNothingLocksBeforeTheFeatureRelease(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha", "2025-01-01T00:00:00Z", "ann@alpha.example")
	if got := e.run(t, "2027-09-15T23:59:59Z"); got != (retention.Result{}) {
		t.Fatalf("locked since 2025 but the feature shipped 2026-10-16: %+v", got)
	}
	if got := e.run(t, "2027-09-16T00:00:00Z"); got.Noticed != 1 {
		t.Fatalf("notice is due 30 days before 2027-10-16: %+v", got)
	}
}

func TestDeletionIsOffUntilSwitchedOn(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.service = e.serviceWith(func(c *retention.Config) { c.DeleteEnabled = false })
	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-02T00:00:00Z"); got.Deleted != 0 || got.Held != 1 || !e.exists(t, a.ID) || !e.blobExists(a.Keys[0]) {
		t.Fatalf("deletion switch off: %+v", got)
	}
	e.service = e.serviceWith(nil)
	if got := e.run(t, "2027-11-02T01:00:00Z"); got.Deleted != 1 {
		t.Fatalf("switched on: %+v", got)
	}
}

func TestDailyDeletionCap(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for _, name := range []string{"One", "Two", "Three"} {
		ids = append(ids, e.workspace(t, name, "2026-11-01T00:00:00Z", strings.ToLower(name)+"@x.example").ID)
	}
	e.service = e.serviceWith(func(c *retention.Config) { c.MaxDeletionsPerDay = 2 })
	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-01T00:00:00Z"); got.Deleted != 2 || got.Held != 1 {
		t.Fatalf("cap of 2: %+v", got)
	}
	if got := e.run(t, "2027-11-01T23:00:00Z"); got.Deleted != 0 || got.Held != 1 {
		t.Fatalf("same day: %+v", got)
	}
	if got := e.run(t, "2027-11-02T00:00:01Z"); got.Deleted != 1 {
		t.Fatalf("next day: %+v", got)
	}
	e.workspace(t, "Four", "2026-11-02T00:00:00Z", "four@x.example")
	e.service = e.serviceWith(func(c *retention.Config) { c.MaxDeletionsPerDay = 0 })
	e.run(t, "2027-10-03T00:00:00Z")
	if got := e.run(t, "2028-06-01T00:00:00Z"); got.Deleted != 0 || got.Held != 1 {
		t.Fatalf("cap of 0 must delete nothing: %+v", got)
	}
}

func TestPolarIsAskedBeforeEveryDeletion(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	e.polar.paid[a.ID] = true
	e.now = at("2027-11-01T00:00:00Z")
	got, err := e.service.Run(context.Background())
	if err == nil || got.Deleted != 0 || !e.exists(t, a.ID) || !e.blobExists(a.Keys[0]) {
		t.Fatalf("Polar says the workspace pays: %+v err %v; want it kept and an error logged", got, err)
	}
	e.polar.paid[a.ID], e.polar.err = false, errors.New("polar down")
	if got, err := e.service.Run(context.Background()); err == nil || got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("Polar error must hold the deletion: %+v err %v", got, err)
	}
	e.polar.err = nil
	if got := e.run(t, "2027-11-01T02:00:00Z"); got.Deleted != 1 || len(e.polar.asks) == 0 || e.polar.asks[0] != a.ID {
		t.Fatalf("asks %v result %+v; want Polar asked by workspace id, then deleted", e.polar.asks, got)
	}
}

func TestRunRefusesWhenTheApplicationAndDatabaseClocksDisagree(t *testing.T) {
	e := newEnv(t)
	e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	skewed := e.serviceWith(func(c *retention.Config) {
		c.Now = nil
		c.SystemNow = func() time.Time { return time.Now().Add(6 * time.Minute) }
	})
	if _, err := skewed.Run(context.Background()); !errors.Is(err, retention.ErrClockSkew) {
		t.Fatalf("Run with a 6 minute skew: %v, want ErrClockSkew", err)
	}
	if _, err := skewed.Plan(context.Background()); !errors.Is(err, retention.ErrClockSkew) {
		t.Fatalf("Plan with a skew: %v, want ErrClockSkew", err)
	}
	healthy := e.serviceWith(func(c *retention.Config) { c.Now = nil })
	if _, err := healthy.Run(context.Background()); err != nil {
		t.Fatalf("Run with matching clocks: %v", err)
	}
}

func TestPartialMailFailureDoesNotResendToAdminsAlreadyNotified(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example", "bob@alpha.example")
	e.mailer.fail = func(to string) error {
		if to == "bob@alpha.example" {
			return errors.New("greylisted")
		}
		return nil
	}
	e.now = at("2027-10-02T00:00:00Z")
	if _, err := e.service.Run(context.Background()); err == nil {
		t.Fatal("the transient failure must be reported")
	}
	if e.noticeSentAt(t, a.ID) != nil {
		t.Fatal("the 30 day clock must not start until every admin is notified")
	}
	e.mailer.fail = nil
	if got := e.run(t, "2027-10-02T01:00:00Z"); got.Noticed != 1 {
		t.Fatalf("second pass: %+v", got)
	}
	counts := map[string]int{}
	for _, sent := range e.mailer.sent {
		counts[sent.To]++
	}
	if counts["ann@alpha.example"] != 1 || counts["bob@alpha.example"] != 1 {
		t.Fatalf("mails per admin = %v, want one each", counts)
	}
	if sent := e.noticeSentAt(t, a.ID); sent == nil || !sent.Equal(at("2027-10-02T01:00:00Z")) {
		t.Fatalf("notice_sent_at = %v, want the time the last admin was reached", sent)
	}
}

func TestPayingWorkspacesAreNotCandidates(t *testing.T) {
	e := newEnv(t)
	paying := e.workspace(t, "Paying", "2026-11-01T00:00:00Z", "p@x.example")
	comp := e.workspace(t, "Comp", "2026-11-01T00:00:00Z", "c@x.example")
	locked := e.workspace(t, "Locked", "2026-11-01T00:00:00Z", "l@x.example")
	e.pay(t, paying.ID, "sub_1", "active", nil)
	if _, err := e.store.GrantCompPlan(context.Background(), comp.ID, domain.PlanTeam, "ops", nil, at("2027-01-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	e.now = at("2028-06-01T00:00:00Z")
	ids, err := e.service.Candidates(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != locked.ID {
		t.Fatalf("Candidates = %v, %v; want only the locked workspace %s", ids, err, locked.ID)
	}
}

func TestNoticeIsRecordedOnlyWhenSomeAdminGotIt(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example", "bob@alpha.example")
	e.mailer.fail = func(string) error { return errors.New("connection refused") }
	e.now = at("2027-10-02T00:00:00Z")
	if _, err := e.service.Run(context.Background()); err == nil {
		t.Fatal("a transient mail failure must be reported")
	}
	if e.noticeSentAt(t, a.ID) != nil {
		t.Fatal("notice recorded although nothing was delivered")
	}
	e.mailer.fail = func(to string) error {
		if to == "ann@alpha.example" {
			return &textproto.Error{Code: 550, Msg: "no such user"}
		}
		return nil
	}
	if got := e.run(t, "2027-10-02T01:00:00Z"); got.Noticed != 1 || e.mailer.count() != 1 || e.mailer.sent[0].To != "bob@alpha.example" {
		t.Fatalf("a permanent failure for one admin must not block the other: %+v mails %+v", got, e.mailer.sent)
	}
	b := e.workspace(t, "Beta", "2026-11-01T00:00:00Z", "carl@beta.example")
	e.mailer.fail = func(string) error { return &textproto.Error{Code: 550, Msg: "no such user"} }
	got := e.run(t, "2027-10-03T00:00:00Z")
	if got.Noticed != 0 || e.noticeSentAt(t, b.ID) != nil {
		t.Fatalf("no deliverable admin: notice recorded anyway: %+v", got)
	}
	if got := e.run(t, "2028-01-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) || !e.exists(t, b.ID) {
		t.Fatalf("only the noticed workspace may be deleted: %+v", got)
	}
}

func TestPlanListsActionsAndChangesNothing(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.workspace(t, "Beta", "2027-03-01T00:00:00Z", "carl@beta.example")
	e.now = at("2027-10-02T00:00:00Z")
	items, err := e.service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Slug != a.Slug || items[0].Action != retention.ActionNotice || !items[0].LockedSince.Equal(at("2026-11-01T00:00:00Z")) || !items[0].DeleteAt.Equal(at("2027-11-01T00:00:00Z")) {
		t.Fatalf("plan = %+v", items)
	}
	if e.mailer.count() != 0 || e.noticeSentAt(t, a.ID) != nil {
		t.Fatal("plan must not send or record")
	}
}

func TestDecide(t *testing.T) {
	since := at("2026-11-01T00:00:00Z")
	sent := func(v string) *time.Time { x := at(v); return &x }
	cases := []struct {
		name   string
		locked bool
		sent   *time.Time
		now    string
		want   retention.Action
	}{
		{"before notice date", true, nil, "2027-10-01T23:59:59Z", retention.ActionNone},
		{"notice date", true, nil, "2027-10-02T00:00:00Z", retention.ActionNotice},
		{"noticed, before deletion date", true, sent("2027-10-02T00:00:00Z"), "2027-10-31T00:00:00Z", retention.ActionNone},
		{"noticed, deletion date", true, sent("2027-10-02T00:00:00Z"), "2027-11-01T00:00:00Z", retention.ActionDelete},
		{"noticed late, too soon", true, sent("2027-10-20T00:00:00Z"), "2027-11-05T00:00:00Z", retention.ActionNone},
		{"noticed late, 30 days passed", true, sent("2027-10-20T00:00:00Z"), "2027-11-19T00:00:00Z", retention.ActionDelete},
		{"stale notice, young lock", true, sent("2026-10-01T00:00:00Z"), "2027-01-01T00:00:00Z", retention.ActionReset},
		{"stale notice, notice due", true, sent("2026-10-01T00:00:00Z"), "2027-10-02T00:00:00Z", retention.ActionNotice},
		{"unlocked, never noticed", false, nil, "2027-10-02T00:00:00Z", retention.ActionNone},
		{"unlocked, noticed", false, sent("2027-10-02T00:00:00Z"), "2027-10-15T00:00:00Z", retention.ActionReset},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retention.Decide(c.locked, since, c.sent, at(c.now)); got != c.want {
				t.Fatalf("Decide = %s, want %s", got, c.want)
			}
		})
	}
}

func (e *env) trashEntries(t *testing.T) int {
	t.Helper()
	entries, err := e.disk.ListTrash(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestTrashEntryOfAnExistingWorkspaceIsRestoredNotPurged(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	// A crash after the rename and before the commit: the row stayed, the files are in the trash.
	if _, _, err := e.disk.Trash(context.Background(), a.ID, at("2027-01-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	e.run(t, "2027-01-01T00:30:00Z") // a delete in another process may still be running
	if e.blobExists(a.Keys[0]) || e.trashEntries(t) != 1 {
		t.Fatal("a fresh trash entry must be left alone")
	}
	e.run(t, "2027-01-01T02:00:00Z")
	if !e.blobExists(a.Keys[0]) || !e.blobExists(a.Keys[1]) || e.trashEntries(t) != 0 {
		t.Fatalf("files not restored for the workspace that still exists (trash entries %d)", e.trashEntries(t))
	}
}

func TestTrashEntryOfAnExistingWorkspaceIsNeverPurgedEvenWhenItCannotBeRestored(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	if _, _, err := e.disk.Trash(context.Background(), a.ID, at("2027-01-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.disk.Put(context.Background(), a.ID, strings.NewReader("newer file"), -1); err != nil {
		t.Fatal(err)
	}
	e.now = at("2027-03-01T00:00:00Z") // far past the 30 day window
	if _, err := e.service.Run(context.Background()); err == nil {
		t.Fatal("a trash entry that cannot be restored must be reported")
	}
	if e.trashEntries(t) != 1 {
		t.Fatal("the entry of an existing workspace was purged")
	}
}

func TestTrashOfADeletedWorkspaceIsPurgedAfterTheWindow(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	e.run(t, "2027-11-01T00:00:00Z")
	if e.exists(t, a.ID) || e.trashEntries(t) != 1 {
		t.Fatal("setup: deleted workspace should have one trash entry")
	}
	e.run(t, "2027-12-01T00:00:01Z")
	if e.trashEntries(t) != 0 {
		t.Fatal("trash of a deleted workspace must be purged after 30 days")
	}
}

func TestRecoveryAfterAnAmbiguousCommitNeverResurrectsADeletedWorkspacesFiles(t *testing.T) {
	e := newEnv(t)
	gone := e.workspace(t, "Gone", "2026-11-01T00:00:00Z", "g@x.example")
	kept := e.workspace(t, "Kept", "2026-11-01T00:00:00Z", "k@x.example")
	ctx := context.Background()
	undoGone, _, err := e.disk.Trash(ctx, gone.ID, at("2027-11-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	undoKept, _, err := e.disk.Trash(ctx, kept.ID, at("2027-11-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `DELETE FROM workspaces WHERE id=$1`, gone.ID) // the commit did reach the database
	if err := e.service.RestoreUnlessDeleted(ctx, gone.ID, undoGone); err != nil {
		t.Fatal(err)
	}
	if e.blobExists(gone.Keys[0]) || e.trashEntries(t) != 2 {
		t.Fatal("files of a deleted workspace were moved back to a place nobody cleans")
	}
	if err := e.service.RestoreUnlessDeleted(ctx, kept.ID, undoKept); err != nil {
		t.Fatal(err)
	}
	if !e.blobExists(kept.Keys[0]) {
		t.Fatal("files of a workspace that still exists must come back")
	}
}

func TestDeletionIsRefusedWhenTheAttachmentDirectoryIsNotWhereTheDatabaseSaysItIs(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	elsewhere, err := attachments.NewLocalDisk(t.TempDir()) // a wrong ATTACHMENTS_DIR
	if err != nil {
		t.Fatal(err)
	}
	wrong := e.serviceWith(func(c *retention.Config) { c.Storage = elsewhere })
	e.now = at("2027-11-01T00:00:00Z")
	if got, err := wrong.Run(context.Background()); err == nil || got.Deleted != 0 || !e.exists(t, a.ID) {
		t.Fatalf("deleted a workspace whose files it could not find: %+v err %v", got, err)
	}
	if !e.blobExists(a.Keys[0]) {
		t.Fatal("files were touched")
	}
}

func TestAWorkspaceWithoutAttachmentsDeletesEvenIfItHasNoDirectory(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.exec(t, `DELETE FROM attachments WHERE workspace_id=$1`, a.ID)
	if err := os.RemoveAll(filepath.Join(e.root, a.ID)); err != nil {
		t.Fatal(err)
	}
	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("workspace with no files: %+v", got)
	}
}

// hookedDisk runs hook inside Trash, that is while the delete holds the workspace row lock.
type hookedDisk struct {
	*attachments.LocalDisk
	hook func()
}

func (d *hookedDisk) Trash(ctx context.Context, workspaceID string, now time.Time) (func() error, bool, error) {
	d.hook()
	return d.LocalDisk.Trash(ctx, workspaceID, now)
}

func TestASecondInstanceFindingTheWorkspaceAlreadyDeletedIsNotAnError(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2027-10-02T00:00:00Z")
	e.now = at("2027-11-01T00:00:00Z")
	second := make(chan error, 1)
	instanceB := e.serviceWith(nil)
	disk := &hookedDisk{LocalDisk: e.disk}
	disk.hook = func() {
		// Instance B read the same candidate list and now waits for the row lock instance A holds.
		go func() { _, err := instanceB.Run(context.Background()); second <- err }()
		time.Sleep(500 * time.Millisecond)
	}
	instanceA := e.serviceWith(func(c *retention.Config) { c.Storage = disk })
	if got, err := instanceA.Run(context.Background()); err != nil || got.Deleted != 1 {
		t.Fatalf("instance A: %+v, %v", got, err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("instance B reported %v for a workspace instance A deleted; want no error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("instance B did not finish")
	}
	if e.exists(t, a.ID) {
		t.Fatal("workspace still exists")
	}
}
