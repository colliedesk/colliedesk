package channeltest_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A customer names a conversation by its code to add to it ("not fixed REF-2026-0170 still jams"): their own
// one-to-one conversation, or one of their group's, opens again. A code of the system before (below where
// this server took the series over) opens a new conversation that says so; any other code (mistyped,
// someone else's, another group's) is answered with the same notice and adds to nothing.
func TestACustomerReopensAConversationByItsCode(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{"reopen": {Action: channels.ActionReopen, Match: channels.MatchWords, Phrases: []string{"not fixed"}}}
	loop.Defaults.Notices.ReopenNotFound = "No request {ref} here."
	loop.Defaults.Notices.ReopenLegacy = "{old_ref} was in the old system: this is {ref} now."
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	year := d.clock.Year()
	next, start, startYear := int64(170), int64(170), year
	if _, err := d.store.SetNumberSeries(ctx, d.workspaceID, "ref", store.NumberSeriesInput{Format: ptr("REF-{yyyy}-{n:04}"), Next: &next, Start: &start, StartYear: &startYear}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: []byte(`{"number_series":"ref"}`)}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "o-1", From: "u1", Text: "the printer jams"})
	d.send(t, channeltest.LoopbackEvent{ID: "o-2", From: "u3", Group: "g1", Text: "the scanner is offline"})
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved' WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	code := func(n int) string { return fmt.Sprintf("REF-%d-%04d", year, n) }
	landed := func(id string) string {
		t.Helper()
		return d.one(t, `SELECT COALESCE((SELECT c.display_ref||':'||c.status FROM messages m JOIN conversations c ON c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.external_id='loopback:'||$2),'-')`, id)
	}
	// Their own, as they type it; a group's, by another member of the group.
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: fmt.Sprintf("not fixed ref-%d-170 still jams", year)})
	d.send(t, channeltest.LoopbackEvent{ID: "r-2", From: "u4", Group: "g1", Text: "not fixed " + code(171)})
	if got := landed("r-1") + "," + landed("r-2"); got != code(170)+":open,"+code(171)+":open" {
		t.Fatalf("reopened = %s", got)
	}
	// Someone else's, another group's, a code this server never gave: one answer, nothing added.
	d.send(t, channeltest.LoopbackEvent{ID: "r-3", From: "u2", Text: "not fixed " + code(170)})
	*d.clock = d.clock.Add(time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "r-4", From: "u5", Group: "g2", Text: "not fixed " + code(171)})
	*d.clock = d.clock.Add(time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "r-5", From: "u1", Text: "not fixed " + code(500)})
	if got := landed("r-3") + "," + landed("r-4") + "," + landed("r-5"); got != "-,-,-" {
		t.Fatalf("not found ones landed in %s", got)
	}
	if got := d.one(t, `SELECT string_agg(i.body,'|' ORDER BY i.occurred_at,i.id) FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.role='bot' AND i.disposition_reason=$2`, channels.NoticeReopenNotFound); got !=
		"No request "+code(170)+" here.|No request "+code(171)+" here.|No request "+code(500)+" here." {
		t.Fatalf("not found notices = %s", got)
	}
	// A code of the system before: below the start this year, or of an earlier year.
	*d.clock = d.clock.Add(time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "r-6", From: "u1", Text: "not fixed " + code(123) + " the printer again"})
	d.send(t, channeltest.LoopbackEvent{ID: "r-7", From: "u1", Text: fmt.Sprintf("not fixed REF-%d-0999", year-1)})
	if got := landed("r-6") + "," + landed("r-7"); got != code(172)+":open,"+code(173)+":open" {
		t.Fatalf("legacy codes landed in %s", got)
	}
	if got := d.one(t, `SELECT string_agg(m.body,'|' ORDER BY m.created_at) FROM messages m WHERE m.workspace_id=$1 AND m.notice_kind=$2`, channels.NoticeReopenLegacy); got !=
		code(123)+" was in the old system: this is "+code(172)+" now.|"+fmt.Sprintf("REF-%d-0999", year-1)+" was in the old system: this is "+code(173)+" now." {
		t.Fatalf("legacy notices = %s", got)
	}
	// After the series' format changes, a code of the system before in the old format is still one, and a
	// code given in the old format still reopens its conversation.
	if _, err := d.store.SetNumberSeries(ctx, d.workspaceID, "ref", store.NumberSeriesInput{Format: ptr("T{n:03}")}); err != nil {
		t.Fatal(err)
	}
	*d.clock = d.clock.Add(time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "r-8", From: "u1", Text: "not fixed " + code(124)})
	*d.clock = d.clock.Add(time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "r-9", From: "u1", Text: "not fixed " + code(170)})
	if got := landed("r-8") + "," + landed("r-9"); got != "T174:open,"+code(170)+":open" {
		t.Fatalf("after the format change = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id WHERE ct.workspace_id=$1 AND t.name='legacy-ref'`); got != "3" {
		t.Fatalf("legacy-ref tags = %s", got)
	}
}
