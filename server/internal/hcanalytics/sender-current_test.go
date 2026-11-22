package hcanalytics_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

func enabledGA4(workspace string) hcanalytics.Target {
	target := ga4Target(workspace)
	target.Enabled = true
	return target
}

func settle() { time.Sleep(80 * time.Millisecond) }

// A periodic read that started before a team turned analytics off must not turn it back on.
func TestAFullReloadThatStartedBeforeATeamTurnedAnalyticsOffKeepsItOff(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}, FlushEvery: time.Hour})
	hold := make(chan struct{})
	store.mu.Lock()
	store.hold, store.listed = hold, make(chan struct{}, 1)
	store.mu.Unlock()
	full := make(chan error, 1)
	go func() { full <- sender.Reload(context.Background(), "") }()
	<-store.listed // the full read holds the old, enabled target
	off := enabledGA4("w1")
	off.Enabled = false
	store.mu.Lock()
	store.targets, store.hold = []hcanalytics.Target{off}, nil
	store.mu.Unlock()
	if err := sender.Reload(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	close(hold)
	if err := <-full; err != nil {
		t.Fatal(err)
	}
	sender.PageViewed(articleView("w1", "after turning it off"))
	settle()
	if hits := ga4.snapshot(); len(hits) != 0 {
		t.Fatalf("GA4 got %d page views after the team turned analytics off", len(hits))
	}
}

// A page view queued, or waiting for a retry, when the team turns analytics off is not sent, and
// the send in flight does not write its result over the new settings.
func TestDeliveriesQueuedBeforeATeamTurnsAnalyticsOffAreNotSent(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	ga4.hold = make(chan struct{})
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		Senders: 1, FlushEvery: time.Hour, Timeout: 10 * time.Second})
	sender.PageViewed(articleView("w1", "in flight"))
	ga4.wait(t, 1)
	sender.PageViewed(articleView("w1", "queued"))
	off := enabledGA4("w1")
	off.Enabled = false
	store.set(off)
	if err := sender.Reload(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	close(ga4.hold)
	settle()
	if hits := ga4.snapshot(); len(hits) != 1 {
		t.Fatalf("GA4 got %d page views, want only the 1 in flight when analytics was turned off", len(hits))
	}
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if results := store.recorded(); len(results) != 0 {
		t.Fatalf("results = %+v, want none: they describe the settings before the save", results)
	}
}

func TestARetryAfterATeamRemovedTheSecretIsNotSent(t *testing.T) {
	ga4 := newStub(t, http.StatusInternalServerError)
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		RetryDelays: []time.Duration{50 * time.Millisecond}, FlushEvery: time.Hour})
	sender.PageViewed(articleView("w1", "fails"))
	ga4.wait(t, 1)
	store.set(hcanalytics.Target{WorkspaceID: "w1", Provider: hcanalytics.GA4, MeasurementID: "G-TEST1234"})
	if err := sender.Reload(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if hits := ga4.snapshot(); len(hits) != 1 {
		t.Fatalf("GA4 got %d requests, want 1: the retry carried the removed secret", len(hits))
	}
}

// One workspace whose service hangs must not hold the senders every other workspace shares.
func TestAWorkspaceWhoseServiceKeepsFailingIsPausedAndOthersStillSend(t *testing.T) {
	hanging := newStub(t, http.StatusAccepted)
	hanging.hold = make(chan struct{})
	defer close(hanging.hold)
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{
		{WorkspaceID: "slow", Provider: hcanalytics.Plausible, Domain: "help.slow.example", BaseURL: hanging.server.URL, Enabled: true},
		enabledGA4("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL,
		AllowDial: map[string]bool{ga4.address(): true, hanging.address(): true},
		Senders:   1, Timeout: 30 * time.Millisecond, RetryDelays: []time.Duration{}, PauseAfter: 3, PauseFor: time.Hour, FlushEvery: time.Hour})
	for range 10 {
		sender.PageViewed(articleView("slow", "slow"))
	}
	hanging.wait(t, 3)
	sender.PageViewed(articleView("w1", "fast"))
	ga4.wait(t, 1)
	if hits := hanging.snapshot(); len(hits) != 3 {
		t.Fatalf("the hanging service got %d requests, want 3: it is paused after 3 failures in a row", len(hits))
	}
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var slow hcanalytics.Result
	for _, result := range store.recorded() {
		if result.WorkspaceID == "slow" {
			slow = result
		}
	}
	if slow.Error != "Plausible did not answer within 30ms." {
		t.Fatalf("the paused workspace's last error = %q, want the service's own failure", slow.Error)
	}

	// Saving the settings again ends the pause.
	store.set(hcanalytics.Target{WorkspaceID: "slow", Provider: hcanalytics.Plausible, Domain: "help.slow.example", BaseURL: hanging.server.URL + "/", Enabled: true}, enabledGA4("w1"))
	if err := sender.Reload(context.Background(), "slow"); err != nil {
		t.Fatal(err)
	}
	sender.PageViewed(articleView("slow", "after saving"))
	hanging.wait(t, 1)
}

// A retry never pushes a newer page view out of a full queue.
func TestARetryThatFindsTheQueueFullIsDroppedNotTheNewerPageView(t *testing.T) {
	failing := newStub(t, http.StatusInternalServerError)
	busy := newStub(t, http.StatusAccepted)
	busy.hold = make(chan struct{})
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1"),
		{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.busy.example", BaseURL: busy.server.URL, Enabled: true}}}
	sender := start(t, store, hcanalytics.Options{GA4Base: failing.server.URL,
		AllowDial: map[string]bool{failing.address(): true, busy.address(): true},
		Senders:   1, QueueSize: 1, Timeout: 10 * time.Second, RetryDelays: []time.Duration{300 * time.Millisecond}, FlushEvery: time.Hour})
	sender.PageViewed(articleView("w1", "first"))
	failing.wait(t, 1)
	sender.PageViewed(articleView("w2", "busy"))
	busy.wait(t, 1) // the one sender now waits on w2
	sender.PageViewed(articleView("w1", "newer"))
	time.Sleep(500 * time.Millisecond) // the retry of "first" finds the queue full
	close(busy.hold)
	failing.wait(t, 2)
	time.Sleep(400 * time.Millisecond)
	var titles []string
	for _, hit := range failing.snapshot() {
		titles = append(titles, hit.body["events"].([]any)[0].(map[string]any)["params"].(map[string]any)["page_title"].(string))
	}
	if len(titles) != 3 || titles[0] != "first" || titles[1] != "newer" || titles[2] != "newer" {
		t.Fatalf("GA4 got %v, want [first newer newer]: the retry of first was dropped and newer kept", titles)
	}
}

func TestTheSameReaderHasAnotherGA4IdInEachWorkspace(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1"), enabledGA4("w3")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}, Senders: 1})
	sender.PageViewed(articleView("w1", "one"))
	ga4.wait(t, 1)
	sender.PageViewed(articleView("w3", "two"))
	hits := ga4.wait(t, 1)
	if hits[0].body["client_id"] != "74a25aed764a4b2d45ae09d8da0d51b5" || hits[1].body["client_id"] != "c1652c56f450e4033a3f92f686be68a3" {
		t.Fatalf("client ids = %v, %v", hits[0].body["client_id"], hits[1].body["client_id"])
	}
}

func TestResultsAFailedWriteCouldNotRecordAreWrittenByTheNextFlush(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1")}, failRecord: 1}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}, FlushEvery: time.Hour})
	sender.PageViewed(articleView("w1", "sent"))
	ga4.wait(t, 1)
	settle()
	if err := sender.Flush(context.Background()); err == nil {
		t.Fatal("the first flush succeeded; the store was set to fail it")
	}
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if results := store.recorded(); len(results) != 1 || results[0].SentAt.IsZero() {
		t.Fatalf("results = %+v, want the send the failed write lost", results)
	}
}

// A target the store cannot read keeps the one already loaded; with none loaded, its problem is
// the workspace's last error.
func TestATargetThatCannotBeReadKeepsTheLoadedOneOrSaysWhy(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{enabledGA4("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}, FlushEvery: time.Hour})
	problem := "The saved GA4 API secret cannot be opened on this server: enter it again and save."
	store.set(hcanalytics.Target{WorkspaceID: "w1", Problem: problem}, hcanalytics.Target{WorkspaceID: "w9", Problem: problem})
	if err := sender.Reload(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	sender.PageViewed(articleView("w1", "still sent"))
	ga4.wait(t, 1)
	settle()
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	results := store.recorded()
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		if result.WorkspaceID == "w9" && (result.Error != problem || result.ErrorAt.IsZero()) {
			t.Fatalf("w9 result = %+v, want its problem as the last error", result)
		}
		if result.WorkspaceID == "w1" && (result.Error != "" || result.SentAt.IsZero()) {
			t.Fatalf("w1 result = %+v, want the send of the target it kept", result)
		}
	}
}
