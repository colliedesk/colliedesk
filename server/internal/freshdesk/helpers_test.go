package freshdesk_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "https://desk.example.com"

type importEnv struct {
	pool        *pgxpool.Pool
	storage     *attachments.LocalDisk
	storageRoot string
	auth        *auth.Service
	fake        *fakeFreshdesk
	workspaceID string
	pairingURL  string
	mu          sync.Mutex
	sleeps      []time.Duration
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	storage, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	service := auth.New(pool, testOrigin)
	workspace, link, err := service.CreateWorkspace(context.Background(), "Acme Support", "alice@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	return &importEnv{pool: pool, storage: storage, storageRoot: root, auth: service, fake: newFakeFreshdesk(t), workspaceID: workspace.ID, pairingURL: link}
}

func (e *importEnv) options() freshdesk.Options {
	options := freshdesk.Options{
		WorkspaceSlug:  "acme-support",
		BaseURL:        e.fake.api.URL,
		APIKey:         testAPIKey,
		Storage:        e.storage,
		MaxTicketPages: 1,
		RateLimitPause: 61 * time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error {
			e.mu.Lock()
			e.sleeps = append(e.sleeps, d)
			e.mu.Unlock()
			return ctx.Err()
		},
	}
	freshdesk.UseTestNetwork(&options, e.fake.roots, e.fake.api.Listener.Addr().String(), e.fake.files.Listener.Addr().String())
	freshdesk.SetProbeAhead(&options, 3)
	return options
}

func (e *importEnv) run(t *testing.T, ctx context.Context, mutate func(*freshdesk.Options)) (*freshdesk.Report, error) {
	t.Helper()
	options := e.options()
	if mutate != nil {
		mutate(&options)
	}
	return freshdesk.Run(ctx, e.pool, options)
}

func (e *importEnv) mustRun(t *testing.T) *freshdesk.Report {
	t.Helper()
	report, err := e.run(t, context.Background(), nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return report
}

func (e *importEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, e.workspaceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *importEnv) tableCounts(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"contacts", "conversations", "messages", "attachments", "time_entries", "tags", "conversation_tags"} {
		parts = append(parts, fmt.Sprintf("%s=%d", table, e.count(t, table)))
	}
	return strings.Join(parts, " ")
}

// target returns the Colliedesk id recorded in the import ledger for a Freshdesk id.
func (e *importEnv) target(t *testing.T, entity, freshdeskID string) string {
	t.Helper()
	var id string
	err := e.pool.QueryRow(context.Background(), `SELECT target_id::text FROM import_items
		WHERE workspace_id=$1 AND entity=$2 AND source_id=$3 AND status='done'`, e.workspaceID, entity, freshdeskID).Scan(&id)
	if err != nil {
		t.Fatalf("ledger target %s %s: %v", entity, freshdeskID, err)
	}
	return id
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL import test")
	}
	schema := "test_fd_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	// A non-UTC session proves timestamps do not depend on the server timezone.
	config.ConnConfig.RuntimeParams["timezone"] = "Asia/Ho_Chi_Minh"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return pool
}
