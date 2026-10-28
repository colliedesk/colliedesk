package channeltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type defaultsDesk struct {
	pool        *pgxpool.Pool
	store       *store.Store
	kind        channels.Type
	workspaceID string
	actor       store.ChannelActor
	accounts    int
}

func newDefaultsDesk(t *testing.T) *defaultsDesk {
	t.Helper()
	loop := channeltest.NewLoopback("loopdef", channels.Sync)
	pool, st := loopbackStore(t, loop)
	workspaceID, agentID := workspaceMaker(pool)(t, "Defaults Desk", "admin@defaults.example.com")
	return &defaultsDesk{pool: pool, store: st, kind: loop.Type, workspaceID: workspaceID, actor: store.ChannelActor{AgentID: agentID}}
}

// add adds a channel, named when name is not empty, with an outside account of its own.
func (d *defaultsDesk) add(name string) (channels.Channel, error) {
	d.accounts++
	input := store.ChannelInput{Config: json.RawMessage(fmt.Sprintf(`{"account":"def-%d"}`, d.accounts))}
	if name != "" {
		input.Name = &name
	}
	return d.store.CreateChannel(context.Background(), d.workspaceID, d.kind, input, d.actor)
}

func (d *defaultsDesk) mustAdd(t *testing.T, name string) channels.Channel {
	t.Helper()
	channel, err := d.add(name)
	if err != nil {
		t.Fatal(err)
	}
	return channel
}

func (d *defaultsDesk) defaults(t *testing.T) []string {
	t.Helper()
	rows, err := d.pool.Query(context.Background(), `SELECT name FROM channels WHERE workspace_id=$1 AND type=$2 AND is_default ORDER BY name`, d.workspaceID, string(d.kind))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func (d *defaultsDesk) lastEvent(t *testing.T) (kind, detail string) {
	t.Helper()
	if err := d.pool.QueryRow(context.Background(), `SELECT kind,detail::text FROM channel_events WHERE workspace_id=$1 ORDER BY created_at DESC,seq DESC LIMIT 1`,
		d.workspaceID).Scan(&kind, &detail); err != nil {
		t.Fatal(err)
	}
	return kind, detail
}

// The first channel of a type is its default; the next ones are not, and get a name of their own
// when none is given.
func TestTheFirstChannelOfATypeIsItsDefaultAndTheNextGetNamesOfTheirOwn(t *testing.T) {
	d := newDefaultsDesk(t)
	first, second, third := d.mustAdd(t, ""), d.mustAdd(t, ""), d.mustAdd(t, "")
	if !first.IsDefault || second.IsDefault || third.IsDefault {
		t.Fatalf("defaults: first=%v second=%v third=%v, want only the first", first.IsDefault, second.IsDefault, third.IsDefault)
	}
	if first.Name != "Loopback" || second.Name != "Loopback 2" || third.Name != "Loopback 3" {
		t.Fatalf("names %q %q %q, want Loopback, Loopback 2, Loopback 3", first.Name, second.Name, third.Name)
	}
}

// Two live channels of a type never share a name, whatever its case; the error names the channel
// that has it. A removed channel's name is free.
func TestAChannelNameIsTakenOnlyByALiveChannelOfTheType(t *testing.T) {
	d := newDefaultsDesk(t)
	sales := d.mustAdd(t, "Sales")
	_, err := d.add("SALES")
	var taken *channels.NameTakenError
	if !errors.As(err, &taken) || taken.ChannelID != sales.ID || taken.ChannelName != "Sales" {
		t.Fatalf("a second Sales: %v, want the name taken by %s", err, sales.ID)
	}
	other := d.mustAdd(t, "Support")
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, other.ID, store.ChannelInput{Name: ptr("sales")}, d.actor); !errors.As(err, &taken) || taken.ChannelID != sales.ID {
		t.Fatalf("rename to sales: %v, want the name taken by %s", err, sales.ID)
	}
	if err := d.store.RemoveChannel(context.Background(), d.workspaceID, sales.ID, d.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := d.add("Sales"); err != nil {
		t.Fatalf("the name of a removed channel: %v", err)
	}
}

// Making a channel the default takes it from the one that had it, records who did it, and only true
// is taken: a type keeps a default while it has a channel.
func TestMakingAChannelTheDefaultTakesItFromTheOther(t *testing.T) {
	d := newDefaultsDesk(t)
	first, second := d.mustAdd(t, "First"), d.mustAdd(t, "Second")
	updated, err := d.store.UpdateChannel(context.Background(), d.workspaceID, second.ID, store.ChannelInput{IsDefault: ptr(true)}, d.actor)
	if err != nil || !updated.IsDefault {
		t.Fatalf("make second the default: %+v %v", updated, err)
	}
	if got := d.defaults(t); fmt.Sprint(got) != "[Second]" {
		t.Fatalf("defaults = %v, want [Second]", got)
	}
	if kind, detail := d.lastEvent(t); kind != "default_changed" || detail != fmt.Sprintf(`{"to": "%s", "from": "%s"}`, second.ID, first.ID) {
		t.Fatalf("last event %s %s", kind, detail)
	}
	var field *channels.FieldError
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, second.ID, store.ChannelInput{IsDefault: ptr(false)}, d.actor); !errors.As(err, &field) || field.Fields["is_default"] == "" {
		t.Fatalf("is_default false: %v, want a field error", err)
	}
}

// Removing the default makes the oldest live channel of the type the default; removing the last
// leaves none.
func TestRemovingTheDefaultHandsItToTheOldestLiveChannel(t *testing.T) {
	d := newDefaultsDesk(t)
	first, second, third := d.mustAdd(t, "First"), d.mustAdd(t, "Second"), d.mustAdd(t, "Third")
	if err := d.store.RemoveChannel(context.Background(), d.workspaceID, first.ID, d.actor); err != nil {
		t.Fatal(err)
	}
	if got := d.defaults(t); fmt.Sprint(got) != "[Second]" {
		t.Fatalf("defaults after removing the default = %v, want [Second]", got)
	}
	if kind, detail := d.lastEvent(t); kind != "default_changed" || detail != fmt.Sprintf(`{"to": "%s", "from": "%s"}`, second.ID, first.ID) {
		t.Fatalf("last event %s %s", kind, detail)
	}
	for _, channel := range []channels.Channel{third, second} {
		if err := d.store.RemoveChannel(context.Background(), d.workspaceID, channel.ID, d.actor); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.defaults(t); len(got) != 0 {
		t.Fatalf("defaults with no channel left = %v", got)
	}
	if again := d.mustAdd(t, ""); !again.IsDefault {
		t.Fatal("the first channel added after all were removed is not the default")
	}
}

// Requests at the same moment leave exactly one default and never two channels of one name.
func TestConcurrentChangesKeepOneDefaultAndDistinctNames(t *testing.T) {
	d := newDefaultsDesk(t)
	ctx := context.Background()
	const n = 8
	inputs := make([]store.ChannelInput, n)
	for i := range inputs {
		inputs[i] = store.ChannelInput{Config: json.RawMessage(fmt.Sprintf(`{"account":"race-%d"}`, i))}
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = d.store.CreateChannel(ctx, d.workspaceID, d.kind, inputs[i], d.actor)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if got := d.defaults(t); len(got) != 1 {
		t.Fatalf("defaults after %d concurrent adds = %v, want one", n, got)
	}
	var names int
	if err := d.pool.QueryRow(ctx, `SELECT count(DISTINCT lower(name)) FROM channels WHERE workspace_id=$1 AND deleted_at IS NULL`, d.workspaceID).Scan(&names); err != nil || names != n {
		t.Fatalf("distinct names = %d (%v), want %d", names, err, n)
	}

	// The same name at once: one wins, the others are told it is taken.
	var taken int
	var mu sync.Mutex
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "Same"
			_, err := d.store.CreateChannel(ctx, d.workspaceID, d.kind, store.ChannelInput{Name: &name,
				Config: json.RawMessage(fmt.Sprintf(`{"account":"same-%d"}`, i))}, d.actor)
			var nameErr *channels.NameTakenError
			mu.Lock()
			defer mu.Unlock()
			if errors.As(err, &nameErr) {
				taken++
			} else if err != nil {
				t.Errorf("create Same %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if taken != n-1 {
		t.Fatalf("%d of %d concurrent adds of one name were refused, want %d", taken, n, n-1)
	}

	// Removing the default while another channel is made the default.
	var live []string
	rows, err := d.pool.Query(ctx, `SELECT id::text FROM channels WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY is_default DESC,created_at`, d.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		live = append(live, id)
	}
	rows.Close()
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := d.store.RemoveChannel(ctx, d.workspaceID, live[0], d.actor); err != nil {
			t.Errorf("remove the default: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := d.store.UpdateChannel(ctx, d.workspaceID, live[len(live)-1], store.ChannelInput{IsDefault: ptr(true)}, d.actor); err != nil {
			t.Errorf("make the last the default: %v", err)
		}
	}()
	wg.Wait()
	if got := d.defaults(t); len(got) != 1 {
		t.Fatalf("defaults after a removal and a change at once = %v, want one", got)
	}
}

func ptr[T any](value T) *T { return &value }
