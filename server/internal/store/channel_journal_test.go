package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func journalEvent(externalID string) channels.Raw {
	return channels.Raw{ExternalEventID: externalID, JSON: json.RawMessage(`{"text":"hello"}`)}
}

// A channel that sends an event again gets the journal row of the first delivery back, named a
// duplicate, whether the deliveries come one after another or at once.
func TestARedeliveredChannelEventIsRecordedOnceAndNamedADuplicate(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	now := time.Now()
	workspace := testWorkspace(t, pool, "journal-one")
	channel := testChannel(t, pool, workspace, "zalo", "Zalo")

	first, duplicate, err := s.RecordInboundEvent(ctx, workspace, channel, journalEvent("evt-1"), now)
	if err != nil || first == "" || duplicate {
		t.Fatalf("first delivery = %q %v %v, want a new row", first, duplicate, err)
	}
	again, duplicate, err := s.RecordInboundEvent(ctx, workspace, channel, journalEvent("evt-1"), now)
	if err != nil || again != first || !duplicate {
		t.Fatalf("second delivery = %q %v %v, want row %s named a duplicate", again, duplicate, err, first)
	}

	const deliveries = 8
	ids := make([]string, deliveries)
	duplicates := make([]bool, deliveries)
	errs := make([]error, deliveries)
	atOnce(deliveries, func(i int) {
		ids[i], duplicates[i], errs[i] = s.RecordInboundEvent(ctx, workspace, channel, journalEvent("evt-2"), now)
	})
	fresh := 0
	for i := range deliveries {
		if errs[i] != nil {
			t.Fatalf("delivery %d at once: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("deliveries at once got rows %v, want one row", ids)
		}
		if !duplicates[i] {
			fresh++
		}
	}
	if fresh != 1 || ids[0] == first {
		t.Fatalf("deliveries at once: %d not duplicates (row %s), want exactly one new row", fresh, ids[0])
	}
}

// An event without an id is never a duplicate, and the same id on another workspace's channel is that
// channel's own event.
func TestAChannelEventWithoutAnIDOrOnAnotherWorkspaceIsNew(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	now := time.Now()
	workspace := testWorkspace(t, pool, "journal-mine")
	channel := testChannel(t, pool, workspace, "zalo", "Zalo")
	otherWorkspace := testWorkspace(t, pool, "journal-theirs")
	otherChannel := testChannel(t, pool, otherWorkspace, "zalo", "Zalo")

	a, dupA, errA := s.RecordInboundEvent(ctx, workspace, channel, journalEvent(""), now)
	b, dupB, errB := s.RecordInboundEvent(ctx, workspace, channel, journalEvent(""), now)
	if errA != nil || errB != nil || dupA || dupB || a == b {
		t.Fatalf("events without an id = (%q %v %v) (%q %v %v), want two new rows", a, dupA, errA, b, dupB, errB)
	}
	mine, _, err := s.RecordInboundEvent(ctx, workspace, channel, journalEvent("evt-shared"), now)
	if err != nil {
		t.Fatal(err)
	}
	theirs, duplicate, err := s.RecordInboundEvent(ctx, otherWorkspace, otherChannel, journalEvent("evt-shared"), now)
	if err != nil || duplicate || theirs == mine {
		t.Fatalf("same id on another workspace = %q %v %v, want a new row of its own", theirs, duplicate, err)
	}
}
