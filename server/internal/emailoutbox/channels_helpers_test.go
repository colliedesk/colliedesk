package emailoutbox_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// channelStore is a store with the server's channel types, as the API builds it.
func channelStore(t *testing.T, pool *pgxpool.Pool) *store.Store {
	t.Helper()
	return store.New(pool).WithChannels(registry.New(registry.Config{}))
}

// ingestEmail hands a parsed message to the workspace's email channel, as the inbound route does,
// keeping its original in a storage of its own.
func ingestEmail(t *testing.T, ctx context.Context, st *store.Store, workspaceID string, parsed email.Inbound) (channels.IngestResult, error) {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		return channels.IngestResult{}, err
	}
	st = st.WithObjects(storage)
	channel, found, err := st.DefaultChannelOfType(ctx, workspaceID, channelemail.Type)
	if err != nil || !found {
		return channels.IngestResult{}, err
	}
	results, err := pipeline.New(st, nil).Receive(ctx, channel, []channels.Raw{{Parsed: parsed}})
	if err != nil {
		return channels.IngestResult{}, err
	}
	return results[0], nil
}

// localStorage is attachment storage of the test's own.
func localStorage(t *testing.T) attachments.Storage {
	t.Helper()
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return storage
}
