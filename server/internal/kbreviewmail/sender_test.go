package kbreviewmail_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/kbreviewmail"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// recordingMailer is the SMTP relay, an external boundary.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
	fail error
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.sent = append(m.sent, to[0]+"\n"+strings.ReplaceAll(string(message), "=\r\n", ""))
	return nil
}

type env struct {
	ctx                       context.Context
	db                        *database.DB
	st                        *store.Store
	workspace, author, long   string
	loan, article, collection string
	mail                      *recordingMailer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := databasetest.Pool(t)
	e := &env{ctx: ctx, db: database.New(pool), st: store.New(pool).WithChannels(registry.New(registry.Config{})), mail: &recordingMailer{}}
	workspace, _, err := auth.NewWithDB(e.db, "https://app.colliedesk.example").CreateWorkspace(ctx, "Northwind Help", "tu@northwind.example.com")
	if err != nil {
		t.Fatal(err)
	}
	e.workspace = workspace.ID
	e.author = e.exec(t, `UPDATE agents SET display_name='Hoang Tu' WHERE workspace_id=$1 RETURNING id::text`, e.workspace)
	e.long = e.publisher(t, "long@northwind.example.com", "Long Bui")
	e.loan = e.publisher(t, "loan@northwind.example.com", "Loan Pham")
	collection, err := e.st.CreateKBCollection(ctx, e.workspace, store.Actor{AgentID: e.author}, store.KBCollectionInput{Name: "Billing"})
	if err != nil {
		t.Fatal(err)
	}
	e.collection = collection.ID
	article, err := e.st.CreateKBArticle(ctx, e.workspace, store.Actor{AgentID: e.author}, store.KBArticleInput{CollectionID: e.collection, Title: "Refunds", BodyMD: "Within 14 days."})
	if err != nil {
		t.Fatal(err)
	}
	e.article = article.ID
	return e
}

// publisher adds an active teammate with the Admin role, who may publish.
func (e *env) publisher(t *testing.T, address, name string) string {
	t.Helper()
	return e.exec(t, `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,$2,$3,'admin') RETURNING id::text`, e.workspace, address, name)
}

func (e *env) exec(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := e.db.Privileged(e.ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, sql, args...).Scan(&out)
	}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return out
}

func (e *env) request(t *testing.T, note string, reviewers ...string) domain.KBReview {
	t.Helper()
	review, err := e.st.RequestKBReview(e.ctx, e.workspace, store.Actor{AgentID: e.author}, e.article, store.KBReviewRequest{ReviewerIDs: reviewers, Note: note})
	if err != nil {
		t.Fatal(err)
	}
	return review
}

// run is one tick of the sender: the emails it handed to the relay.
func (e *env) run(t *testing.T) []string {
	t.Helper()
	sender := kbreviewmail.Sender{Store: e.st, Mailer: e.mail, From: "noreply@mail.colliedesk.example", Origin: "https://app.colliedesk.example"}
	if _, err := sender.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.mail.mu.Lock()
	defer e.mail.mu.Unlock()
	sent := e.mail.sent
	e.mail.sent = nil
	return sent
}

func assertMail(t *testing.T, mail string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(mail, want) {
			t.Errorf("email lacks %q:\n%s", want, mail)
		}
	}
}

// Each reviewer gets one email on the first tick, with who asked, the note and a link to the review, and
// the review records when they were told; the author is told of the decision once.
func TestEachReviewerIsEmailedOnceAndTheAuthorHearsTheDecision(t *testing.T) {
	e := newEnv(t)
	review := e.request(t, "Please check the refund window.", e.long, e.loan)

	sent := e.run(t)
	if len(sent) != 2 {
		t.Fatalf("sent %d emails, want 2: %q", len(sent), sent)
	}
	byTo := map[string]string{}
	for _, mail := range sent {
		byTo[mail[:strings.Index(mail, "\n")]] = mail
	}
	for _, to := range []string{"long@northwind.example.com", "loan@northwind.example.com"} {
		assertMail(t, byTo[to], "Subject: Hoang Tu asked you to review: Refunds", `"Refunds"`, "Northwind Help",
			"Please check the refund window.", "https://app.colliedesk.example/knowledge/reviews/"+review.ID, "Reviews list")
	}
	if notified := e.exec(t, `SELECT count(*)::text FROM kb_review_reviewers WHERE review_id=$1 AND email='sent' AND notified_at IS NOT NULL`, review.ID); notified != "2" {
		t.Fatalf("reviewers recorded as told = %s, want 2", notified)
	}
	if again := e.run(t); len(again) != 0 {
		t.Fatalf("a second tick sent again: %q", again)
	}

	if _, err := e.st.DecideKBReview(e.ctx, e.workspace, store.Actor{AgentID: e.long}, review.ID,
		store.KBReviewDecision{State: domain.KBReviewChangesRequested, Note: "Say business days."}); err != nil {
		t.Fatal(err)
	}
	decided := e.run(t)
	if len(decided) != 1 {
		t.Fatalf("after the decision sent %d emails, want 1: %q", len(decided), decided)
	}
	assertMail(t, decided[0], "tu@northwind.example.com\n", "Subject: Long Bui asked for changes: Refunds", "Say business days.",
		"https://app.colliedesk.example/knowledge/articles/"+e.article)
	if again := e.run(t); len(again) != 0 {
		t.Fatalf("a second tick after the decision sent again: %q", again)
	}
}

// An edit while the review is open tells the reviewers the text changed; a review withdrawn before its
// email went, and a reviewer deactivated since, get none.
func TestAChangedTextIsEmailedAgainAndAClosedReviewIsNot(t *testing.T) {
	e := newEnv(t)
	first := e.request(t, "", e.long)
	e.run(t)
	article, err := e.st.GetKBArticle(e.ctx, e.workspace, e.article)
	if err != nil {
		t.Fatal(err)
	}
	body := "Within 30 days."
	if _, err := e.st.UpdateKBArticle(e.ctx, e.workspace, store.Actor{AgentID: e.author}, e.article, article.UpdatedAt, store.KBArticleChange{BodyMD: &body}); err != nil {
		t.Fatal(err)
	}
	next := e.exec(t, `SELECT id::text FROM kb_reviews WHERE supersedes_review_id=$1`, first.ID)
	again := e.run(t)
	if len(again) != 1 {
		t.Fatalf("after the edit sent %d emails, want 1: %q", len(again), again)
	}
	assertMail(t, again[0], "long@northwind.example.com\n", "Subject: Review again: Refunds", "changed",
		"https://app.colliedesk.example/knowledge/reviews/"+next)

	if _, err := e.st.DecideKBReview(e.ctx, e.workspace, store.Actor{AgentID: e.author}, next, store.KBReviewDecision{State: domain.KBReviewWithdrawn}); err != nil {
		t.Fatal(err)
	}
	withdrawn := e.request(t, "", e.loan, e.long)
	e.exec(t, `UPDATE agents SET deactivated_at=now() WHERE id=$1 RETURNING id::text`, e.loan)
	if _, err := e.st.DecideKBReview(e.ctx, e.workspace, store.Actor{AgentID: e.author}, withdrawn.ID, store.KBReviewDecision{State: domain.KBReviewWithdrawn}); err != nil {
		t.Fatal(err)
	}
	if sent := e.run(t); len(sent) != 0 {
		t.Fatalf("a withdrawn review was emailed: %q", sent)
	}
	if off := e.exec(t, `SELECT count(*)::text FROM kb_review_reviewers WHERE review_id=$1 AND email='off'`, withdrawn.ID); off != "2" {
		t.Fatalf("emails of the withdrawn review turned off = %s, want 2", off)
	}
}

// A send that fails for now goes again on a later tick.
func TestAFailedSendGoesAgainLater(t *testing.T) {
	e := newEnv(t)
	review := e.request(t, "", e.long)
	e.mail.fail = errors.New("relay unavailable")
	if sent := e.run(t); len(sent) != 0 {
		t.Fatalf("sent while the relay was down: %q", sent)
	}
	if state := e.exec(t, `SELECT email FROM kb_review_reviewers WHERE review_id=$1`, review.ID); state != "queued" {
		t.Fatalf("after a failed send email=%s, want queued", state)
	}
	e.mail.fail = nil
	if sent := e.run(t); len(sent) != 1 {
		t.Fatalf("after the relay came back sent %d, want 1", len(sent))
	}
}
