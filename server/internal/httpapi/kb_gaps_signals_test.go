package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type gapSignal struct {
	kind, text, title, status string
	askers                    int
}

// gapSignalsAfterOneRun groups what is queued, once, and reads back the signals of the workspace with
// their gaps.
func gapSignalsAfterOneRun(t *testing.T, pool *pgxpool.Pool, workspace string) []gapSignal {
	t.Helper()
	if _, err := store.New(pool).BuildKBGaps(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(context.Background(), `SELECT s.kind, s.text, g.title, g.status, g.askers
		FROM kb_gap_signals s JOIN kb_gaps g ON g.workspace_id=s.workspace_id AND g.id=s.gap_id
		WHERE s.workspace_id=$1 ORDER BY s.id`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var signals []gapSignal
	for rows.Next() {
		var s gapSignal
		if err := rows.Scan(&s.kind, &s.text, &s.title, &s.status, &s.askers); err != nil {
			t.Fatal(err)
		}
		signals = append(signals, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return signals
}

func wantOneGapSignal(t *testing.T, signals []gapSignal, want gapSignal) {
	t.Helper()
	if len(signals) != 1 || signals[0] != want {
		t.Fatalf("gap signals = %+v, want only %+v", signals, want)
	}
}

func TestANoResultSearchOnTheHelpCenterBecomesAGapSignal(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Gap Search", "admin@gap-search.example.com"))
	wantPage(t, visit(d.site, "/help/"+d.slug+"/search?q=refund+policy", "192.0.2.71", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, "/help/"+d.slug+"/search?q=password", "192.0.2.71", "User-Agent", browserAgent), http.StatusOK, nil, nil)

	wantOneGapSignal(t, gapSignalsAfterOneRun(t, pool, d.id), gapSignal{kind: "search_no_result", text: "refund policy", title: "refund policy", status: "open", askers: 1})
}

func TestANoVoteOnTheHelpCenterBecomesAMaskedGapSignalTheReaderCannotRead(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	d := newSearchSite(t, api, pool, createSession(t, api, service, "Gap Vote", "admin@gap-vote.example.com"))
	no := postVote(d.site, d.page+"/vote", url.Values{"helpful": {"no"}}, testOrigin, "192.0.2.72")
	wantSeeOther(t, no, d.page+"?voted=no#helpful")
	line := url.Values{"helpful": {"no"}, "missing": {"How do I reset it without my email jane.doe@example.com or code 4821993"}}
	wantSeeOther(t, postVote(d.site, d.page+"/vote", line, testOrigin, "192.0.2.72", chatCookie(t, no)), d.page+"?voted=sent#helpful")

	masked := "How do I reset it without my email [email removed] or code [number removed]"
	wantOneGapSignal(t, gapSignalsAfterOneRun(t, pool, d.id), gapSignal{kind: "vote_no", text: masked, title: masked, status: "open", askers: 1})
	reader, err := database.New(pool).ReaderRole(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"kb_gap_pending", "kb_gaps", "kb_gap_signals"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var granted bool
			if err := pool.QueryRow(context.Background(), `SELECT has_table_privilege($1, $2, $3)`, reader, table, privilege).Scan(&granted); err != nil {
				t.Fatal(err)
			}
			if granted {
				t.Errorf("the help center reader has %s on %s", privilege, table)
			}
		}
	}
}

func TestAHandedOffHelpChatBecomesAGapSignal(t *testing.T) {
	d := newChatDesk(t, "Gap Chat", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	first := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "Can I change the delivery address?"})
	chatEvents(t, first)
	if response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "reader@example.com"}, chatCookie(t, first)); response.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", response.Code, response.Body.String())
	}

	signals := gapSignalsAfterOneRun(t, d.pool, d.ws)
	want := gapSignal{kind: "chat_handoff", text: "Can I change the delivery address?", title: "Can I change the delivery address?", status: "open", askers: 1}
	wantOneGapSignal(t, signals, want)
}
