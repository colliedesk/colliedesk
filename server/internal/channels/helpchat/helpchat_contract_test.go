package helpchat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A hand-off threads by the chat it came from (a reply to one is the same chat) and is counted by the
// address it came from.
func TestHelpChatPassesTheChannelContract(t *testing.T) {
	pool := databasetest.Pool(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(pool).WithObjects(storage).WithChannels(registry.New(registry.Config{}))
	agents := map[string]string{}
	workspace := func(t *testing.T, name, adminEmail string) (string, string) {
		t.Helper()
		created, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(context.Background(), name, adminEmail)
		if err != nil {
			t.Fatal(err)
		}
		var agentID string
		if err := pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1`, created.ID).Scan(&agentID); err != nil {
			t.Fatal(err)
		}
		agents[created.ID] = agentID
		return created.ID, agentID
	}
	channeltest.Run(t, channeltest.Harness{Type: helpchat.Type, Pool: pool, Store: st, Workspace: workspace,
		Connect: func(t *testing.T, workspaceID string, n int) channels.Channel {
			t.Helper()
			channel, err := st.CreateChannel(context.Background(), workspaceID, helpchat.Type, store.ChannelInput{
				Config: json.RawMessage(`{"greeting":"Hi","widget_origins":["https://site` + fmt.Sprint(n) + `.example"]}`)}, store.ChannelActor{AgentID: agents[workspaceID]})
			if err != nil {
				t.Fatal(err)
			}
			return channel
		},
		Raw: func(t *testing.T, _ channels.Channel, spec channeltest.Spec) channels.Raw {
			session := spec.ID
			if spec.Answers != "" {
				session = spec.Answers
			}
			body, err := json.Marshal(helpchat.Handoff{SessionID: "contract-" + session, Email: spec.From + "@customer.example", FirstQuestion: "Where is my order?",
				Transcript: "Customer: " + spec.Text + "\nAI: The articles do not say.", Address: helpchat.AddressKey("w", spec.From)})
			if err != nil {
				t.Fatal(err)
			}
			return channels.Raw{JSON: body}
		},
	})
}
