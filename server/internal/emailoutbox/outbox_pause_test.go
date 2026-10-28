package emailoutbox_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func pauseWorkspace(t *testing.T, f fixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET email_paused_at=now(),email_pause_reason='3 spam complaints on 100 replies in the last 7 days.' WHERE id=$1`, f.workspaceID); err != nil {
		t.Fatal(err)
	}
}

func TestPausedWorkspaceRepliesAreDeferredNotSentAndNoAttemptIsSpent(t *testing.T) {
	f := newFixture(t)
	other := addWorkspace(t, f.pool, "Other Desk", "other.example.com")
	server := startSMTPServer(t, "250 OK")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	paused := f.write(t, "outgoing", "Held back.")
	free := other.write(t, "outgoing", "Goes out.")
	pauseWorkspace(t, f)

	process(t, worker)
	held := f.delivery(t, paused.ID)
	if held.Status != "queued" || held.Attempts != 0 || held.SentAt != nil || held.LastError == nil || !strings.Contains(*held.LastError, "paused after spam complaints") {
		t.Fatalf("a paused workspace's reply must wait with the reason and no attempt spent: %+v", held)
	}
	if sent := other.delivery(t, free.ID); sent.Status != "sent" {
		t.Fatalf("another workspace must keep sending: %+v", sent)
	}
	if mails := server.mail(); len(mails) != 1 || mails[0].From != "support@other.example.com" {
		t.Fatalf("only the other workspace may reach the relay: %+v", mails)
	}

	// Deferred rows stay due later without being failed, however long the pause lasts.
	for range 12 {
		clock = clock.Add(2 * time.Hour)
		process(t, worker)
	}
	if held = f.delivery(t, paused.ID); held.Status != "queued" || held.Attempts != 0 {
		t.Fatalf("a long pause must not fail or exhaust the reply: %+v", held)
	}
}

func TestResumingSendingDeliversTheHeldReplyAtOnce(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t, "250 OK")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Held back.")
	pauseWorkspace(t, f)
	process(t, worker)

	resumed, err := f.store.ResumeEmailSending(context.Background(), f.workspaceID)
	if err != nil || resumed == nil || !strings.Contains(resumed.Reason, "3 spam complaints") {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	if again, err := f.store.ResumeEmailSending(context.Background(), f.workspaceID); err != nil || again != nil {
		t.Fatalf("resuming a workspace that is not paused changes nothing: %+v %v", again, err)
	}
	clock = clock.Add(time.Minute)
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || delivery.Attempts != 1 {
		t.Fatalf("held reply after resume: %+v", delivery)
	}
}

func nextAttempt(t *testing.T, f fixture, messageID string) time.Time {
	t.Helper()
	var at time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT next_attempt_at FROM channel_outbox WHERE message_id=$1`, messageID).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestResumingOnlyReleasesRepliesTheHoldDelayedNotOnesInErrorBackoff(t *testing.T) {
	f := newFixture(t)
	failing := startSMTPServer(t, "451 4.3.0 Try again later")
	clock := time.Now().UTC().Add(time.Second)
	backingOff := f.write(t, "outgoing", "Will back off.")
	process(t, newWorker(f, failing, &clock))
	before := nextAttempt(t, f, backingOff.ID)
	if !before.After(clock.Add(10 * time.Second)) {
		t.Fatalf("setup: the 451 reply should be waiting in backoff, next attempt %s", before)
	}

	held := f.write(t, "outgoing", "Held by the pause.")
	pauseWorkspace(t, f)
	process(t, newWorker(f, failing, &clock))
	if nextAttempt(t, f, held.ID).Before(clock.Add(5 * time.Minute)) {
		t.Fatal("setup: the pause should have deferred the second reply")
	}

	if _, err := f.store.ResumeEmailSending(context.Background(), f.workspaceID); err != nil {
		t.Fatal(err)
	}
	if got := nextAttempt(t, f, backingOff.ID); !got.Equal(before) {
		t.Fatalf("resume cut short a backoff the pause did not cause: %s -> %s", before, got)
	}
	if got := nextAttempt(t, f, held.ID); got.After(time.Now().Add(time.Second)) {
		t.Fatalf("the reply held by the pause must be due at once, next attempt %s", got)
	}
}
