package channeltest_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// joinOpenOnly sets the desk's channel to join only open conversations (Zalo's default).
func (d loopDesk) joinOpenOnly(t *testing.T) {
	t.Helper()
	if _, err := d.pool.Exec(context.Background(), `UPDATE channels SET settings=settings||'{"join_policy":{"kind":"open_only"}}'::jsonb WHERE id=$1`, d.channel.ID); err != nil {
		t.Fatal(err)
	}
}

// With a channel that joins only open conversations, what the customer writes after their conversation
// was resolved opens a new one, by the place's thread or a reply to an old message alike; the resolved one
// gets nothing and stays resolved. Its reopen command still reopens it.
func TestAResolvedConversationGetsNothingWhenTheChannelJoinsOnlyOpenOnes(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	d.joinOpenOnly(t)
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Text: "the stock export fails"})
	first := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Text: "still failing"})
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "1" {
		t.Fatalf("an open conversation was not joined: %s conversations", got)
	}
	if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET status='resolved' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-3", From: "a", Text: "another question"})
	d.send(t, channeltest.LoopbackEvent{ID: "a-4", From: "a", Text: "about the export", Reference: "a-1"})
	if got := d.one(t, `SELECT status||'/'||(SELECT count(*) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id)::text
		FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2`, first); got != "resolved/2" {
		t.Fatalf("the resolved conversation = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1 AND id<>$2 AND status='open'`, first); got != "1" {
		t.Fatalf("new open conversations = %s", got)
	}
}

// AI intake, at suggest and at auto, offers no resolved conversation to join when the channel joins only
// open ones; a person (or AI) joining a card to one resolved meanwhile opens a new conversation instead.
func TestIntakeNeverJoinsAResolvedConversationWhenTheChannelJoinsOnlyOpenOnes(t *testing.T) {
	for _, level := range []string{"suggest", "auto"} {
		t.Run(level, func(t *testing.T) {
			d := newAIDesk(t, intake.OpenGate)
			d.joinOpenOnly(t)
			d.intakeAt(t, level)
			d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
			d.flush(t)
			d.answers(map[string]any{"action": "create", "subject": "Stock export fails", "confidence": 0.97})
			d.runAI(t)
			card := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 ORDER BY created_at LIMIT 1`)
			if level == "suggest" {
				if _, err := d.intake().Decide(context.Background(), d.workspaceID, card, intake.Decision{Action: intake.Create, Subject: "Stock export fails"},
					store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
					t.Fatal(err)
				}
			}
			conversation := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
			if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET status='resolved' WHERE id=$1`, conversation); err != nil {
				t.Fatal(err)
			}
			d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "it fails again"})
			d.flush(t)
			next := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 AND state IN ('deciding','review') ORDER BY created_at DESC LIMIT 1`)
			detail, err := d.store.IntakeGatheringByID(context.Background(), d.workspaceID, next, *d.clock)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range detail.Candidates {
				if candidate.ConversationID == conversation {
					t.Fatalf("the resolved conversation is offered: %+v", detail.Candidates)
				}
			}
			// A person following AI's attach to it, made before it was resolved: a new conversation starts.
			if _, err := d.intake().Decide(context.Background(), d.workspaceID, next, intake.Decision{Action: intake.Attach, ConversationID: conversation},
				store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
				t.Fatal(err)
			}
			if got := d.one(t, `SELECT status||'/'||(SELECT count(*) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming')::text
				FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2`, conversation); got != "resolved/1" {
				t.Fatalf("the resolved conversation = %s", got)
			}
			if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1 AND id<>$2`, conversation); got != "1" {
				t.Fatalf("new conversations = %s", got)
			}
		})
	}
}
