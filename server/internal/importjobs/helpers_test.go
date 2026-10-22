package importjobs_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testKey = "fd-job-key-canary-DO-NOT-PRINT"

type workspace struct{ id, slug, agentID string }

type env struct {
	pool    *pgxpool.Pool
	db      *database.DB
	keys    *importjobs.Keys
	service *importjobs.Service
	a, b    workspace
}

func ring(t *testing.T) tokenseal.Keys {
	t.Helper()
	return ringWithID(t, "k1")
}

func ringWithID(t *testing.T, id string) tokenseal.Keys {
	t.Helper()
	raw := make([]byte, tokenseal.KeySize)
	_, _ = rand.Read(raw)
	keys, err := tokenseal.ParseKeys(id + ":" + base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// newEnv makes two workspaces; sealed chooses a server with a key ring.
func newEnv(t *testing.T, sealed bool) *env {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	service := auth.New(pool, "https://desk.example.com")
	made := []workspace{}
	for _, name := range []string{"Acme Support", "Other Team"} {
		ws, _, err := service.CreateWorkspace(context.Background(), name, "admin@"+strings.ReplaceAll(strings.ToLower(name), " ", "")+".example")
		if err != nil {
			t.Fatal(err)
		}
		w := workspace{id: ws.ID, slug: ws.Slug}
		if err := pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1`, ws.ID).Scan(&w.agentID); err != nil {
			t.Fatal(err)
		}
		made = append(made, w)
	}
	var keys *importjobs.Keys
	if sealed {
		keys = importjobs.NewKeys(ring(t))
	} else {
		keys = importjobs.NewKeys(tokenseal.Keys{})
	}
	db := database.New(pool)
	return &env{pool: pool, db: db, keys: keys, service: importjobs.NewService(db, keys), a: made[0], b: made[1]}
}

// exec runs SQL as the owner, outside any workspace scope; tests use it to arrange states.
func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (e *env) column(t *testing.T, id, expression string) any {
	t.Helper()
	var value any
	if err := e.pool.QueryRow(context.Background(), `SELECT `+expression+` FROM import_jobs WHERE id=$1`, id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (e *env) keyErased(t *testing.T, id string) bool {
	t.Helper()
	return e.column(t, id, "sealed_key IS NULL AND sealed_key_id IS NULL").(bool)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL import job test")
	}
	name := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_")
	if len(name) > 30 {
		name = name[:30]
	}
	schema := fmt.Sprintf("test_ij_%s%d", name, time.Now().UnixNano())
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

func ringless() tokenseal.Keys { return tokenseal.Keys{} }
