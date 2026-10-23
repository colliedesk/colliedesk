// Package databasetest gives each test its own PostgreSQL database, cloned from a template that already
// holds every migration. Applying the migrations into a fresh schema took about 250 ms per test and
// DROP SCHEMA ... CASCADE took a lock per object; CREATE DATABASE ... TEMPLATE copies the files in a
// few milliseconds and DROP DATABASE takes no per-object locks. Only test files import it.
//
// The template is named after a hash of the migration files and the connecting role, so a changed
// migration builds a new one, and processes that run at once (go test runs packages in parallel)
// build it once between them under an advisory lock. Nothing may connect to the template: a clone
// fails while anyone is connected, so it is marked ALLOW_CONNECTIONS false once built.
//
// The migration that sets up row-level security names the app role after the database it runs in,
// so every clone shares the template's role, as every test schema in TEST_DATABASE_URL shared one
// before. Tests of the migrations themselves (internal/database) and of `collie migrate` still run
// them for real, in an EmptySchema or their own schema.
package databasetest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// version changes the template name when the way it is built changes.
const version = "1"

var (
	setupOnce sync.Once
	setupErr  error
	admin     *pgxpool.Pool // on TEST_DATABASE_URL: creates and drops the test databases
	template  string
	counter   atomic.Int64
)

// BaseURL returns TEST_DATABASE_URL, and skips t when it is unset.
func BaseURL(t testing.TB) string {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL test")
	}
	return baseURL
}

// URL returns the connection URL of a new database with every migration applied. It is dropped when
// t ends; pools on it must be closed by then (WITH (FORCE) ends any left open).
func URL(t testing.TB) string {
	t.Helper()
	db, err := Create(context.Background(), BaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Drop(context.Background()); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	return db.URL
}

// Pool returns a pool on a new migrated database (URL), closed and dropped when t ends.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return PoolWith(t, nil)
}

// PoolWith is Pool with configure applied to the pool's config first (a tracer, a session
// timezone); nil changes nothing.
func PoolWith(t testing.TB, configure func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(URL(t))
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(config)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close) // runs before the drop URL registered
	return pool
}

// EmptySchema returns TEST_DATABASE_URL with search_path set to a new schema that holds nothing, for
// tests that run the migrations themselves or need a database that was never migrated. The schema
// is dropped when t ends.
func EmptySchema(t testing.TB) string {
	t.Helper()
	baseURL := BaseURL(t)
	ctx := context.Background()
	pool, err := adminPool(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := uniqueName("test_")
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop test schema %s: %v", schema, err)
		}
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Database is one test database cloned from the migrated template.
type Database struct {
	Name string
	URL  string
}

// Create clones the migrated template into a new database, building the template first when no
// process has built it for these migrations yet. The caller drops it.
func Create(ctx context.Context, baseURL string) (*Database, error) {
	setupOnce.Do(func() { setupErr = setup(ctx, baseURL) })
	if setupErr != nil {
		return nil, setupErr
	}
	name := uniqueName("ctest_")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+template); err != nil {
		return nil, fmt.Errorf("clone the migrated template %s: %w", template, err)
	}
	databaseURL, err := withDatabase(baseURL, name)
	if err != nil {
		return nil, err
	}
	return &Database{Name: name, URL: databaseURL}, nil
}

// Drop removes the database, ending any connection still open to it.
func (d *Database) Drop(ctx context.Context) error {
	_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+d.Name+" WITH (FORCE)")
	return err
}

func adminPool(ctx context.Context, baseURL string) (*pgxpool.Pool, error) {
	setupOnce.Do(func() { setupErr = setup(ctx, baseURL) })
	return admin, setupErr
}

func setup(ctx context.Context, baseURL string) error {
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("connect to TEST_DATABASE_URL: %w", err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		pool.Close()
		return fmt.Errorf("connect to TEST_DATABASE_URL: %w", err)
	}
	sum, err := migrationsHash(role)
	if err != nil {
		pool.Close()
		return err
	}
	name := "colliedesk_tpl_" + sum[:16]
	if err := ensureTemplate(ctx, pool, baseURL, name); err != nil {
		pool.Close()
		return err
	}
	admin, template = pool, name
	return nil
}

// migrationsHash covers every migration file by name and content, the role that owns the objects
// (the privileged_access policies name it) and version.
func migrationsHash(role string) (string, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return "", fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	hash := sha256.New()
	fmt.Fprintf(hash, "testdb %s\nrole %s\n", version, role)
	for _, name := range names {
		contents, err := migrations.Files.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read migration %s: %w", name, err)
		}
		fmt.Fprintf(hash, "%s %d\n", name, len(contents))
		hash.Write(contents)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ensureTemplate builds the template under a session advisory lock on TEST_DATABASE_URL's database,
// so concurrent test processes wait for the first one instead of building it twice. A database of
// that name that is not yet a template was left by a build that died, and is built again.
func ensureTemplate(ctx context.Context, pool *pgxpool.Pool, baseURL, name string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("connect to TEST_DATABASE_URL: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('colliedesk-testdb-template',0))`); err != nil {
		return fmt.Errorf("lock the test template: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('colliedesk-testdb-template',0))`) //nolint:errcheck
	var exists, ready bool
	if err := conn.QueryRow(ctx, `SELECT count(*) > 0, coalesce(bool_or(datistemplate), false) FROM pg_database WHERE datname=$1`, name).Scan(&exists, &ready); err != nil {
		return fmt.Errorf("look up the test template: %w", err)
	}
	if ready {
		return nil
	}
	if exists {
		if _, err := conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("drop the unfinished test template %s: %w", name, err)
		}
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE template0"); err != nil {
		return fmt.Errorf("create the test template %s: %w", name, err)
	}
	templateURL, err := withDatabase(baseURL, name)
	if err != nil {
		return err
	}
	migrated, err := pgxpool.New(ctx, templateURL)
	if err != nil {
		return fmt.Errorf("connect to the test template %s: %w", name, err)
	}
	err = database.Migrate(ctx, migrated)
	migrated.Close()
	if err != nil {
		return fmt.Errorf("migrate the test template %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, "ALTER DATABASE "+name+" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false"); err != nil {
		return fmt.Errorf("mark the test template %s: %w", name, err)
	}
	return nil
}

// withDatabase returns baseURL connecting to database name instead.
func withDatabase(baseURL, name string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", fmt.Errorf("TEST_DATABASE_URL must be a postgres:// URL")
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

// uniqueName is unique across the processes on one server (pid, counter) and across runs (time,
// random bits), and stays well under PostgreSQL's 63-byte limit on names.
func uniqueName(prefix string) string {
	random := make([]byte, 3)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%s%d_%d_%d_%s", prefix, os.Getpid(), counter.Add(1), time.Now().Unix()%100000, hex.EncodeToString(random))
}
