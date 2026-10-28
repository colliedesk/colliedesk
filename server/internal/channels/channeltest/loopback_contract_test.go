package channeltest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// loopbackStore returns a database and a store whose registry runs the loopback types.
func loopbackStore(t *testing.T, loops ...*channeltest.Loopback) (*pgxpool.Pool, *store.Store) {
	t.Helper()
	pool := databasetest.Pool(t)
	adapters := make([]channels.Adapter, 0, len(loops))
	for _, loop := range loops {
		if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(loop.Type)); err != nil {
			t.Fatal(err)
		}
		adapters = append(adapters, loop)
	}
	registry, err := channels.NewRegistry(adapters...)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := tokenseal.ParseKeys("k1:" + base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return pool, store.New(pool).WithChannels(registry).WithObjects(storage).WithSealKeys(keys)
}

func loopbackHarness(t *testing.T, loop *channeltest.Loopback) channeltest.Harness {
	pool, st := loopbackStore(t, loop)
	return channeltest.Harness{Type: loop.Type, Pool: pool, Store: st, Secret: "loop-token-1234", Workspace: workspaceMaker(pool),
		Connect: func(t *testing.T, workspaceID string, n int) channels.Channel {
			t.Helper()
			var agentID string
			if err := pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1`, workspaceID).Scan(&agentID); err != nil {
				t.Fatal(err)
			}
			channel, err := st.CreateChannel(context.Background(), workspaceID, loop.Type,
				store.ChannelInput{Config: json.RawMessage(fmt.Sprintf(`{"account":"acct-%d"}`, n))}, store.ChannelActor{AgentID: agentID})
			if err != nil {
				t.Fatal(err)
			}
			return channel
		},
		Raw: func(t *testing.T, channel channels.Channel, spec channeltest.Spec) channels.Raw {
			event := channeltest.LoopbackEvent{ID: spec.ID, From: spec.From, Name: spec.From, Text: spec.Text, Reference: spec.Answers}
			if spec.File != "" {
				event.Files = []channeltest.LoopbackFile{{Name: "file.txt", Type: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(spec.File))}}
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			return channels.Raw{ExternalEventID: spec.ID, JSON: encoded}
		},
	}
}

func TestAsyncLoopbackPassesTheChannelContract(t *testing.T) {
	channeltest.Run(t, loopbackHarness(t, channeltest.NewLoopback("loopback", channels.Async)))
}

func TestSyncLoopbackPassesTheChannelContract(t *testing.T) {
	channeltest.Run(t, loopbackHarness(t, channeltest.NewLoopback("loopback_sync", channels.Sync)))
}

// workspaceMaker creates workspaces with their first admin the way pairing does.
func workspaceMaker(pool *pgxpool.Pool) func(t *testing.T, name, adminEmail string) (string, string) {
	return func(t *testing.T, name, adminEmail string) (string, string) {
		t.Helper()
		ctx := context.Background()
		workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(ctx, name, adminEmail)
		if err != nil {
			t.Fatal(err)
		}
		var agentID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agentID); err != nil {
			t.Fatal(err)
		}
		return workspace.ID, agentID
	}
}
