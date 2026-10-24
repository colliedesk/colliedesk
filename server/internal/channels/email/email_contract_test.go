package email_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	channelemail "github.com/colliedesk/colliedesk/server/internal/channels/email"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	mailpkg "github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// relay accepts every message, like an SMTP relay that works.
type relay struct {
	mu   sync.Mutex
	sent []string
}

func (r *relay) SendCopying(_ context.Context, _, to string, _ []string, message []byte) ([]mailpkg.RefusedCopy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, to)
	_ = message
	return nil, nil
}

func deskStore(t *testing.T) (*pgxpool.Pool, *store.Store) {
	t.Helper()
	pool := databasetest.Pool(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policy := store.OutboundPolicy{Enabled: true, GmailEnabled: true}
	st := store.New(pool).WithObjects(storage).WithOutbound(policy).
		WithChannels(registry.New(registry.Config{Email: channelemail.Config{Outbound: policy}}))
	return pool, st
}

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

// message writes a customer's email of a spec: From <spec.From>@customer.example, its Message-ID from
// the spec's id, and a text attachment when the spec has a file.
func message(spec channeltest.Spec) []byte {
	headers := []string{"From: " + spec.From + " <" + spec.From + "@customer.example>", "To: support@desk.example.com",
		"Subject: Contract", "Message-ID: <" + spec.ID + "@customer.example>"}
	if spec.Answers != "" {
		headers = append(headers, "In-Reply-To: <"+spec.Answers+"@customer.example>")
	}
	if spec.File == "" {
		return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + spec.Text + "\r\n")
	}
	headers = append(headers, "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="b"`)
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\n" + spec.Text +
		"\r\n--b\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=\"file.txt\"\r\n\r\n" + spec.File + "\r\n--b--\r\n")
}

func TestEmailPassesTheChannelContract(t *testing.T) {
	pool, st := deskStore(t)
	channeltest.Run(t, channeltest.Harness{Type: channelemail.Type, Pool: pool, Store: st, Workspace: workspaceMaker(pool),
		Sender: emailoutbox.New(st, &relay{}, emailoutbox.Options{FromPolicy: mailpkg.FromPolicyAny}),
		Connect: func(t *testing.T, workspaceID string, n int) channels.Channel {
			t.Helper()
			if _, err := st.UpsertEmailChannel(context.Background(), workspaceID, fmt.Sprintf("support%d@desk.example.com", n), nil); err != nil {
				t.Fatal(err)
			}
			channel, found, err := st.LiveChannelOfType(context.Background(), workspaceID, channelemail.Type)
			if err != nil || !found {
				t.Fatalf("email channel: %v %v", found, err)
			}
			return channel
		},
		Raw: func(t *testing.T, channel channels.Channel, spec channeltest.Spec) channels.Raw {
			t.Helper()
			raw := message(spec)
			parsed, err := mailpkg.Parse(raw, "")
			if err != nil {
				t.Fatal(err)
			}
			parsed.Platform = true
			return channels.Raw{Parsed: parsed, Body: raw}
		},
	})
}
