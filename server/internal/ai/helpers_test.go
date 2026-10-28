package ai_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testKey = "sk-ai-canary-DO-NOT-PRINT-9f3a"

type workspace struct{ id, agentID string }

type env struct {
	pool    *pgxpool.Pool
	service *ai.Service
	fake    *aiprovider.Fake
	// specs records what each adapter was built from.
	mu    sync.Mutex
	specs []ai.Spec
	a, b  workspace
}

func ring(t *testing.T) tokenseal.Keys {
	t.Helper()
	raw := make([]byte, tokenseal.KeySize)
	_, _ = rand.Read(raw)
	keys, err := tokenseal.ParseKeys("k1:" + base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// newEnv makes two workspaces and a service whose providers are one recording fake.
func newEnv(t *testing.T, mutate func(*ai.Config)) *env {
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
		w := workspace{id: ws.ID}
		if err := pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1`, ws.ID).Scan(&w.agentID); err != nil {
			t.Fatal(err)
		}
		made = append(made, w)
	}
	e := &env{pool: pool, fake: &aiprovider.Fake{}, a: made[0], b: made[1]}
	config := ai.Config{Keys: ring(t), Build: func(spec ai.Spec) (aiprovider.Provider, error) {
		e.mu.Lock()
		e.specs = append(e.specs, spec)
		e.mu.Unlock()
		return e.fake, nil
	}}
	if mutate != nil {
		mutate(&config)
	}
	e.service = ai.NewService(database.New(pool), config)
	return e
}

func (e *env) actor(w workspace) ai.Actor { return ai.Actor{AgentID: w.agentID} }

func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) add(t *testing.T, w workspace, label string) ai.Provider {
	t.Helper()
	provider, err := e.service.AddProvider(context.Background(), w.id, e.actor(w), ai.ProviderInput{
		Kind: "anthropic", Label: label, APIKey: testKey, Models: []string{"claude-sonnet-5-5", "claude-haiku-5-5"}})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return databasetest.Pool(t)
}
