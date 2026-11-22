package hcanalytics_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

// After a restart the saved targets apply at once, not only after the first periodic reload.
func TestASenderLoadsTheSavedTargetsWhenItStarts(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := hcanalytics.New(store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		Origin: "https://desk.example.com", FlushEvery: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sender.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for len(ga4.snapshot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no page view reached GA4 within 5 seconds of the sender starting: it did not load the saved targets")
		}
		sender.PageViewed(articleView("w1", "Reset a password"))
		time.Sleep(20 * time.Millisecond)
	}
}
