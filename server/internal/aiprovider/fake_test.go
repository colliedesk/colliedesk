package aiprovider

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestFakeRecordsEveryRequestAndFollowsItsScript(t *testing.T) {
	fake := &Fake{}
	scripted := &Error{Code: ErrorRateLimited, Status: 429}
	fake.Script(FakeReply{Err: scripted}, FakeReply{Response: Response{Text: "second", Stop: StopDone}})

	if _, err := fake.Complete(context.Background(), draftRequest()); !errors.Is(err, scripted) {
		t.Fatalf("first call error = %v", err)
	}
	if response, _ := fake.Complete(context.Background(), draftRequest()); response.Text != "second" {
		t.Fatalf("second call = %+v", response)
	}
	if response, _ := fake.Complete(context.Background(), draftRequest()); response.JSON != `{"ok":true}` || !response.Usage.Reported {
		t.Fatalf("default answer = %+v", response)
	}
	if got := fake.Requests(); len(got) != 3 || got[0].Messages[0].Text != "customer text: my order is late" {
		t.Fatalf("requests = %+v", got)
	}
}

func TestFakeIsSafeForConcurrentUse(t *testing.T) {
	fake := &Fake{}
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = fake.Check(context.Background(), "model")
		}()
	}
	wait.Wait()
	if got := len(fake.Requests()); got != 20 {
		t.Fatalf("recorded %d requests, want 20", got)
	}
}
