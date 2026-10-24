package webform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/channels/webform"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func TestWebFormPassesTheChannelContract(t *testing.T) {
	pool := databasetest.Pool(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(pool).WithObjects(storage).WithChannels(registry.New(registry.Config{}))
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
		return created.ID, agentID
	}
	channeltest.Run(t, channeltest.Harness{Type: webform.Type, Pool: pool, Store: st, Workspace: workspace,
		Connect: func(t *testing.T, workspaceID string, n int) channels.Channel {
			t.Helper()
			if _, err := st.SetWebForm(context.Background(), workspaceID, true, []string{fmt.Sprintf("https://site%d.example", n)}); err != nil {
				t.Fatal(err)
			}
			channel, found, err := st.LiveChannelOfType(context.Background(), workspaceID, webform.Type)
			if err != nil || !found {
				t.Fatalf("web form channel: %v %v", found, err)
			}
			return channel
		},
		Raw: func(t *testing.T, _ channels.Channel, spec channeltest.Spec) channels.Raw {
			body, err := json.Marshal(webform.Submission{Email: spec.From + "@customer.example", Message: spec.Text, Source: "contract"})
			if err != nil {
				t.Fatal(err)
			}
			return channels.Raw{JSON: body}
		},
	})
}
