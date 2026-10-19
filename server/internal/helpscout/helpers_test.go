package helpscout_test

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
	"github.com/colliedesk/colliedesk/server/internal/helpscout"
	"github.com/jackc/pgx/v5/pgxpool"
)

type importEnv struct {
	pool        *pgxpool.Pool
	storage     *attachments.LocalDisk
	storageRoot string
	fake        *fakeHelpScout
	workspaceID string
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
	workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(context.Background(), "Acme Support", "alice@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	return &importEnv{pool: pool, storage: storage, storageRoot: root, fake: newFakeHelpScout(t), workspaceID: workspace.ID}
}

func (e *importEnv) options() helpscout.Options {
	options := helpscout.Options{
		WorkspaceSlug: "acme-support",
		BaseURL:       e.fake.api.URL,
		AppID:         testAppID,
		AppSecret:     testAppSecret,
		Storage:       e.storage,
		Sleep: func(ctx context.Context, d time.Duration) error {
			e.mu.Lock()
			e.sleeps = append(e.sleeps, d)
			e.mu.Unlock()
			return ctx.Err()
		},
	}
	helpscout.UseTestNetwork(&options, e.fake.roots, e.fake.api.Listener.Addr().String(), e.fake.images.Listener.Addr().String())
	return options
}

func (e *importEnv) run(t *testing.T, mutate func(*helpscout.Options)) (*helpscout.Report, error) {
	t.Helper()
	options := e.options()
	if mutate != nil {
		mutate(&options)
	}
	return helpscout.Run(context.Background(), e.pool, options)
}

func (e *importEnv) mustRun(t *testing.T) *helpscout.Report {
	t.Helper()
	report, err := e.run(t, nil)
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
	for _, table := range []string{"contacts", "conversations", "messages", "attachments", "tags", "conversation_tags"} {
		parts = append(parts, fmt.Sprintf("%s=%d", table, e.count(t, table)))
	}
	return strings.Join(parts, " ")
}

// target returns the Colliedesk id the import ledger holds for a Help Scout id.
func (e *importEnv) target(t *testing.T, entity, helpScoutID string) string {
	t.Helper()
	var id string
	err := e.pool.QueryRow(context.Background(), `SELECT target_id::text FROM import_items
		WHERE workspace_id=$1 AND source='helpscout:mailbox:501' AND entity=$2 AND source_id=$3 AND status='done'`, e.workspaceID, entity, helpScoutID).Scan(&id)
	if err != nil {
		t.Fatalf("ledger target %s %s: %v", entity, helpScoutID, err)
	}
	return id
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL import test")
	}
	schema := "test_hs_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
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

// assertLastPublic checks every conversation's last_public_* against its newest message that is not a note.
func (e *importEnv) assertLastPublic(t *testing.T) {
	t.Helper()
	var wrong int
	err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations c LEFT JOIN LATERAL (
			SELECT m.kind, m.created_at FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind<>'note'
			ORDER BY m.created_at DESC, m.id DESC LIMIT 1) l ON true
		WHERE c.workspace_id=$1 AND (c.last_public_kind IS DISTINCT FROM l.kind OR c.last_public_at IS DISTINCT FROM l.created_at)`, e.workspaceID).Scan(&wrong)
	if err != nil || wrong != 0 {
		t.Fatalf("conversations whose last_public differs from their messages = %d (%v), want 0", wrong, err)
	}
}
