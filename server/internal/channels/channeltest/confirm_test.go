package channeltest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

// confirmLoop is a chat whose one-to-one requests are asked about before they open: a command starts
// one, what follows within 10 seconds joins it, then the channel asks "send OK"; OK or a minute of
// silence opens it, "Hủy" throws it away.
func confirmLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{
		"bug":     {Match: channels.MatchWords, Phrases: []string{"report bug"}},
		"confirm": {Action: channels.ActionConfirm, Match: channels.MatchExact, Phrases: []string{"ok", "xong"}},
		"cancel":  {Action: channels.ActionCancel, Match: channels.MatchExact, Phrases: []string{"Hủy"}},
	}
	loop.Defaults.Notices.AskConfirm = "Is that all? Send OK to open your request, or add more."
	loop.Defaults.Notices.Cancelled = "Cancelled."
	loop.Defaults.Scopes = map[string]channels.Scope{"user": {Mode: channels.ModeCommands,
		Gather: &channels.Gather{QuietSeconds: 10, MaxSeconds: 1800, ConfirmSeconds: 60}}}
	return loop
}

func (d loopDesk) step(t *testing.T, by time.Duration) {
	t.Helper()
	*d.clock = d.clock.Add(by)
	if _, err := d.pipeline.ProcessDue(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (d loopDesk) notices(t *testing.T, kind string) string {
	t.Helper()
	return d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1 AND role='bot' AND disposition_reason=$2`, kind)
}

func (d loopDesk) conversations(t *testing.T) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(replace(m.body,E'\n','+'),'|' ORDER BY m.created_at),'') FROM messages m WHERE m.workspace_id=$1 AND m.kind='incoming'`)
}

// After a quiet window the channel asks; OK opens the request at once, without the OK itself.
func TestAOneToOneRequestIsAskedAboutAndOpensOnOK(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	d.step(t, 11*time.Second)
	if got := d.notices(t, channels.NoticeAskConfirm); got != "1" {
		t.Fatalf("asks = %s, want 1", got)
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("opened before OK: %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "OK"})
	d.step(t, time.Second)
	if got := d.conversations(t); got != "report bug the printer jams" {
		t.Fatalf("after OK = %q", got)
	}
	if got := d.one(t, `SELECT disposition||'/'||COALESCE(disposition_reason,'') FROM channel_thread_items WHERE workspace_id=$1 AND external_id='c-2'`); got != "not_request/confirmed" {
		t.Fatalf("the OK = %s", got)
	}
}

// A minute without an answer opens it as well; what the customer adds after the question joins it and the
// question is asked again once they are quiet.
func TestAOneToOneRequestOpensAfterAMinuteAndTakesWhatIsAdded(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	d.step(t, 11*time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "on tray two"})
	d.step(t, 11*time.Second)
	if got := d.notices(t, channels.NoticeAskConfirm); got != "2" {
		t.Fatalf("asks = %s, want one more after the addition", got)
	}
	d.step(t, 30*time.Second)
	if got := d.conversations(t); got != "" {
		t.Fatalf("opened before the minute: %s", got)
	}
	d.step(t, 31*time.Second)
	if got := d.conversations(t); got != "report bug the printer jams+on tray two" {
		t.Fatalf("after the minute = %q", got)
	}
}

// "Hủy" throws the request away and says so; an OK with no request being written is an ordinary message.
func TestAOneToOneRequestIsCancelledAndALoneOKIsNoConfirmation(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "Hủy"})
	d.step(t, time.Hour)
	if got := d.conversations(t); got != "" {
		t.Fatalf("a cancelled request opened: %s", got)
	}
	if got := d.notices(t, channels.NoticeCancelled); got != "1" {
		t.Fatalf("cancelled notices = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u1", Text: "ok"})
	d.step(t, time.Hour)
	if got := d.one(t, `SELECT disposition FROM channel_thread_items WHERE workspace_id=$1 AND external_id='c-3'`); got == "not_request" {
		t.Fatalf("a lone OK was taken as a confirmation")
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("a lone OK opened %s", got)
	}
}

// An OK written before the question is part of what the customer writes: the channel still asks once they
// are quiet, and the OK stays in the request.
func TestAnOKBeforeTheQuestionIsPartOfTheRequest(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	d.step(t, 2*time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "OK"})
	d.step(t, 11*time.Second)
	if got := d.notices(t, channels.NoticeAskConfirm); got != "1" {
		t.Fatalf("asks = %s, want 1", got)
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("an early OK opened it: %s", got)
	}
	d.step(t, 61*time.Second)
	if got := d.conversations(t); got != "report bug the printer jams+OK" {
		t.Fatalf("after the minute = %q", got)
	}
}

func describingLoop() *channeltest.Loopback {
	loop := confirmLoop()
	bug := loop.Defaults.Commands["bug"]
	bug.AskDescription = "Describe the problem, please."
	loop.Defaults.Commands["bug"] = bug
	loop.Defaults.Notices.Expired = "That request expired. Write report bug to start again."
	loop.Defaults.Scopes["user"].Gather.ExpireSeconds = 1800
	return loop
}

// A command alone asks for a description, not for an OK, and opens nothing until there is one.
func TestABareCommandAsksForADescriptionAndNeverOpensEmpty(t *testing.T) {
	d := newLoopDesk(t, describingLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug"})
	d.step(t, 11*time.Second)
	d.step(t, 20*time.Minute)
	if got := d.notices(t, channels.NoticeAskDescription) + "," + d.notices(t, channels.NoticeAskConfirm); got != "1,0" {
		t.Fatalf("description,confirm asks = %s", got)
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("an empty request opened: %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "the printer jams"})
	d.step(t, 11*time.Second)
	if got := d.notices(t, channels.NoticeAskConfirm); got != "1" {
		t.Fatalf("confirm asks = %s, want 1 once described", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u1", Text: "ok"})
	d.step(t, time.Second)
	if got := d.conversations(t); got != "report bug+the printer jams" {
		t.Fatalf("after OK = %q", got)
	}
}

// Left without a description past expire_seconds, the request is thrown away: the customer's next message
// is told so, and a new command starts a new one.
func TestABareCommandExpiresAndTheNextMessageIsTold(t *testing.T) {
	d := newLoopDesk(t, describingLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug"})
	d.step(t, 11*time.Second)
	d.step(t, 31*time.Minute)
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "the printer jams"})
	d.step(t, time.Hour)
	if got := d.notices(t, channels.NoticeExpired); got != "1" {
		t.Fatalf("expired notices = %s", got)
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("an expired request opened: %s", got)
	}
	if got := d.one(t, `SELECT string_agg(external_event_id||'='||COALESCE(drop_reason,state),',' ORDER BY external_event_id) FROM channel_inbound_events WHERE workspace_id=$1`); got != "c-1=expired,c-2=expired" {
		t.Fatalf("journal = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u1", Text: "report bug the scanner"})
	d.step(t, 11*time.Second)
	d.step(t, 61*time.Second)
	if got := d.conversations(t); got != "report bug the scanner" {
		t.Fatalf("a new request = %q", got)
	}
}

// A request longer than ten thousand characters opens whole.
func TestALongRequestOpensWhole(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	part := strings.Repeat("x", 4000)
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug " + part})
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: part})
	d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u1", Text: part})
	d.step(t, 11*time.Second)
	d.step(t, 61*time.Second)
	if got := d.one(t, `SELECT char_length(body)::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != fmt.Sprint(len("report bug ")+3*4000+2) {
		t.Fatalf("body length = %s", got)
	}
}

// A request is called by what follows its command, not by the command: a command alone takes its name from
// the description that follows.
func TestARequestIsCalledByWhatFollowsItsCommand(t *testing.T) {
	d := newLoopDesk(t, describingLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug: the printer jams"})
	d.step(t, 11*time.Second)
	d.step(t, 61*time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u2", Text: "report bug"})
	d.step(t, 11*time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u2", Text: "the scanner is offline\nsince Monday"})
	d.step(t, 11*time.Second)
	d.step(t, 61*time.Second)
	if got := d.one(t, `SELECT string_agg(subject,'|' ORDER BY created_at) FROM conversations WHERE workspace_id=$1`); got != "the printer jams|the scanner is offline" {
		t.Fatalf("subjects = %q", got)
	}
}

// After the question, what the customer adds sends them back to writing: an OK before the question is
// asked again is part of the request, the question comes once more, and the OK after it opens at once.
// "oke", which the confirm command's exact phrases do not name, is words too.
func TestAnOKAfterAnAdditionWaitsForTheQuestionAgain(t *testing.T) {
	for _, first := range []string{"OK", "oke"} {
		d := newLoopDesk(t, confirmLoop())
		d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
		d.step(t, 11*time.Second)
		d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "them thong tin"})
		d.step(t, 2*time.Second)
		d.send(t, channeltest.LoopbackEvent{ID: "c-3", From: "u1", Text: first})
		d.step(t, time.Second)
		if got := d.one(t, `SELECT count(*)::text FROM channel_inbound_events WHERE workspace_id=$1 AND state='dropped'`); got != "0" {
			t.Fatalf("%s: dropped events while the request is still written = %s", first, got)
		}
		if got := d.conversations(t); got != "" {
			t.Fatalf("%s: opened before the question was asked again: %s", first, got)
		}
		d.step(t, 11*time.Second)
		if got := d.notices(t, channels.NoticeAskConfirm); got != "2" {
			t.Fatalf("%s: asks = %s, want one more", first, got)
		}
		d.send(t, channeltest.LoopbackEvent{ID: "c-4", From: "u1", Text: "ok"})
		d.step(t, time.Second)
		if got := d.conversations(t); got != "report bug the printer jams+them thong tin+"+first {
			t.Fatalf("%s: after the OK = %q", first, got)
		}
	}
}
