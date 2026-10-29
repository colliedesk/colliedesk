package serverconfig_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testRing = "k1:dGVzdC1zZXJ2ZXItY29uZmlnLWtleS0zMi1ieXRlcyE="

func ring(t *testing.T, value string) tokenseal.Keys {
	t.Helper()
	keys, err := tokenseal.ParseKeys(value)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

var cli = serverconfig.Actor{Kind: "cli", Name: "cli:test"}

// acceptAll is a group check that every group passes.
func acceptAll(string, func(string) string) error { return nil }

func newStore(t *testing.T, pool *pgxpool.Pool, keys tokenseal.Keys, environ map[string]string) *serverconfig.Store {
	t.Helper()
	return serverconfig.NewStore(database.New(pool), keys, env(environ))
}

func TestSaveSealsSecretsAndNeverWritesTheirValue(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	store := newStore(t, pool, ring(t, testRing), nil)
	const canary = "smtp-password-canary-7f3a"
	version, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"smtp.host": "smtp.example.net", "smtp.password": canary}, Source: "cli", Actor: cli})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != version || loaded.Getenv("SMTP_PASSWORD") != canary || loaded.Getenv("SMTP_HOST") != "smtp.example.net" {
		t.Fatalf("loaded v%d host=%q, want v%d with the password opened", loaded.Version, loaded.Getenv("SMTP_HOST"), version)
	}
	if loaded.Meta["smtp.password"].KeyID != "k1" || loaded.Meta["smtp.password"].By != "cli:test" {
		t.Fatalf("meta = %+v", loaded.Meta["smtp.password"])
	}
	var dump []byte
	if err := pool.QueryRow(ctx, `SELECT convert_to(
		(SELECT string_agg(coalesce(value,'') || encode(coalesce(sealed,''),'escape'), '|') FROM server_config_values) ||
		(SELECT string_agg(keys::text || detail::text, '|') FROM operator_audit), 'UTF8')`).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dump, []byte(canary)) {
		t.Fatal("a secret's value reached the database in plain text")
	}
	if !bytes.Contains(dump, []byte("smtp.password")) {
		t.Fatal("the audit log does not name the secret that changed")
	}
}

func TestSecretsNeedAKeyRing(t *testing.T) {
	store := newStore(t, databasetest.Pool(t), tokenseal.Keys{}, map[string]string{"SMTP_PASSWORD": "from-env"})
	_, err := store.Save(context.Background(), serverconfig.Change{Set: map[string]string{"smtp.password": "x"}, Source: "cli", Actor: cli})
	if !errors.Is(err, serverconfig.ErrNoRing) {
		t.Fatalf("err = %v, want ErrNoRing", err)
	}
	loaded, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Without a ring the secret stays in the environment (D8), and it is not a leftover.
	if loaded.Getenv("SMTP_PASSWORD") != "from-env" {
		t.Fatalf("SMTP_PASSWORD = %q, want the environment's", loaded.Getenv("SMTP_PASSWORD"))
	}
	if left := serverconfig.Leftovers(loaded.Snapshot, env(map[string]string{"SMTP_PASSWORD": "from-env"})); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestEverySaveIsAVersionThatRestores(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	first, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"smtp.host": "a.example.net", "smtp.password": "one"}, Source: "cli", Actor: cli})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(ctx, serverconfig.Change{Base: first, Set: map[string]string{"smtp.host": "b.example.net", "smtp.password": "two"}, Unset: []string{"smtp.port"}, Source: "console", Actor: cli})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx, serverconfig.Change{Base: first, Set: map[string]string{"smtp.host": "c.example.net"}, Source: "console", Actor: cli}); !errors.Is(err, serverconfig.ErrConflict) {
		t.Fatalf("save on an old version: err = %v, want ErrConflict", err)
	}
	if _, err := store.Save(ctx, serverconfig.Change{Base: second, Set: map[string]string{"smtp.host": "b.example.net"}, Source: "console", Actor: cli}); !errors.Is(err, serverconfig.ErrNoChange) {
		t.Fatalf("save of the same value: err = %v, want ErrNoChange", err)
	}
	restored, err := store.Restore(ctx, first, second, cli, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != restored || loaded.Getenv("SMTP_HOST") != "a.example.net" || loaded.Getenv("SMTP_PASSWORD") != "one" {
		t.Fatalf("after restore: v%d host %q", loaded.Version, loaded.Getenv("SMTP_HOST"))
	}
	history, err := store.History(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Source != "restore" || strings.Join(history[0].Changed, ",") != "smtp.host,smtp.password" {
		t.Fatalf("history = %+v", history)
	}
}

func TestUnknownKeysAreRefused(t *testing.T) {
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	_, err := store.Save(context.Background(), serverconfig.Change{Set: map[string]string{"smtp.hots": "x"}, Source: "cli", Actor: cli})
	if !errors.Is(err, serverconfig.ErrUnknownKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportEnvByGroupAllOrNothing(t *testing.T) {
	ctx := context.Background()
	environ := map[string]string{
		// email: valid, imported whole, secret sealed
		"SMTP_HOST": "smtp.example.net", "SMTP_PORT": "2525", "SMTP_PASSWORD": "pw",
		// microsoft: only the compose file's default; nothing to import
		"MICROSOFT_OAUTH_TENANT": "common",
		// google: invalid as a group; nothing written
		"GOOGLE_OAUTH_CLIENT_ID": "id-only",
		// polar: products become rows
		"POLAR_ACCESS_TOKEN": "tok", "POLAR_WEBHOOK_SECRET": "whsec", "POLAR_PRODUCT_TEAM_MONTHLY": "p-m", "POLAR_PRODUCT_TEAM_YEARLY": "p-y", "POLAR_PRODUCT_FOUNDING": "p-f",
		"SIGNUP_HASH_KEY": "an-operator-chosen-key-of-32-characters!",
	}
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), environ)
	check := func(group string, getenv func(string) string) error {
		if group == "google" && getenv("GOOGLE_OAUTH_CLIENT_SECRET") == "" {
			return errors.New("GOOGLE_OAUTH_CLIENT_SECRET is missing")
		}
		return nil
	}
	result, err := store.ImportEnv(ctx, env(environ), check, serverconfig.Actor{Kind: "server", Name: "serve vtest (env)"}, serverconfig.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Imported, ",") != "email,polar" || result.Invalid["google"] == "" || result.SignupKey != "imported" {
		t.Fatalf("result = %+v", result)
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"SMTP_HOST": "smtp.example.net", "SMTP_PORT": "2525", "SMTP_PASSWORD": "pw", "GOOGLE_OAUTH_CLIENT_ID": "",
		"MICROSOFT_OAUTH_TENANT": "", "POLAR_PRODUCT_TEAM_MONTHLY": "p-m", "POLAR_PRODUCT_FOUNDING": "p-f", "SIGNUP_HASH_KEY": environ["SIGNUP_HASH_KEY"]} {
		if got := loaded.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	// Run again (every start does): the database wins, nothing new is written.
	again, err := store.ImportEnv(ctx, env(map[string]string{"SMTP_HOST": "changed.example.net"}), check, serverconfig.Actor{Kind: "server", Name: "serve"}, serverconfig.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Imported) != 0 || again.SignupKey != "kept" {
		t.Fatalf("second import = %+v", again)
	}
	if left := serverconfig.Leftovers(loaded.Snapshot, env(map[string]string{"SMTP_HOST": "changed.example.net", "SMTP_PORT": "2525"})); len(left) != 2 || left[0].Same || !left[1].Same {
		t.Fatalf("leftovers = %+v, want SMTP_HOST different and SMTP_PORT the same", left)
	}
}

func TestSignupHashKeyImportedBeforeGenerated(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	store := newStore(t, pool, ring(t, testRing), nil)
	const key = "kept-from-the-environment-0123456789"
	result, err := store.ImportEnv(ctx, env(map[string]string{"SIGNUP_HASH_KEY": key}), acceptAll, cli, serverconfig.ImportOptions{})
	if err != nil || result.SignupKey != "imported" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	loaded, _ := store.Current(ctx)
	if loaded.Getenv("SIGNUP_HASH_KEY") != key {
		t.Fatal("the environment's key was not the one stored")
	}

	fresh := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	result, err = fresh.ImportEnv(ctx, env(nil), acceptAll, cli, serverconfig.ImportOptions{})
	if err != nil || result.SignupKey != "generated" {
		t.Fatalf("fresh result = %+v, %v", result, err)
	}
	loaded, _ = fresh.Current(ctx)
	if len(loaded.Getenv("SIGNUP_HASH_KEY")) < 32 {
		t.Fatal("generated key is too short")
	}

	short := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	result, err = short.ImportEnv(ctx, env(map[string]string{"SIGNUP_HASH_KEY": "short"}), acceptAll, cli, serverconfig.ImportOptions{})
	if err != nil || result.SignupKey != "too-short" {
		t.Fatalf("short result = %+v, %v", result, err)
	}
}

func TestOverlappingStartsImportOnce(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	environ := map[string]string{"SMTP_HOST": "smtp.example.net", "INTERNAL_SIGNUP_EMAILS": "@hclabs.co"}
	var wait sync.WaitGroup
	results := make([]serverconfig.ImportResult, 2)
	for index := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			store := newStore(t, pool, ring(t, testRing), environ)
			result, err := store.ImportEnv(ctx, env(environ), acceptAll, cli, serverconfig.ImportOptions{})
			if err != nil {
				t.Error(err)
			}
			results[index] = result
		}()
	}
	wait.Wait()
	var versions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM server_config_versions`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 || len(results[0].Imported)+len(results[1].Imported) != 2 {
		t.Fatalf("versions = %d, results = %+v; want each group imported once", versions, results)
	}
}

// Secrets set while the server had no key ring stay in the environment (D8); the first start with a
// ring imports them, even into a group the database already holds.
func TestSecretsImportedWhenRingAddedLater(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		group  string
		plain  map[string]string
		secret map[string]string
	}{
		{"email", map[string]string{"SMTP_HOST": "smtp.example.net"}, map[string]string{"SMTP_PASSWORD": "pw", "MAIL_FROM_INBOUND_SECRET": strings.Repeat("s", 32), "RESEND_API_KEY": "re", "RESEND_WEBHOOK_SECRET": "wh"}},
		{"ai-server", map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_MODELS": "draft=m"}, map[string]string{"AI_SERVER_API_KEY": "sk"}},
		{"google", map[string]string{"GOOGLE_OAUTH_CLIENT_ID": "id"}, map[string]string{"GOOGLE_OAUTH_CLIENT_SECRET": "gs"}},
		{"microsoft", map[string]string{"MICROSOFT_OAUTH_CLIENT_ID": "id"}, map[string]string{"MICROSOFT_OAUTH_CLIENT_SECRET": "ms"}},
		{"github", map[string]string{"GITHUB_APP_ID": "1", "GITHUB_APP_SLUG": "s", "GITHUB_APP_CLIENT_ID": "c"}, map[string]string{"GITHUB_APP_PRIVATE_KEY": "pem", "GITHUB_APP_CLIENT_SECRET": "cs", "GITHUB_APP_WEBHOOK_SECRET": "ws"}},
		{"polar", map[string]string{"POLAR_API_URL": "https://sandbox-api.polar.sh", "POLAR_PRODUCT_TEAM_MONTHLY": "m"}, map[string]string{"POLAR_ACCESS_TOKEN": "tok", "POLAR_WEBHOOK_SECRET": "whsec"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			pool := databasetest.Pool(t)
			environ := map[string]string{}
			for name, value := range tc.plain {
				environ[name] = value
			}
			for name, value := range tc.secret {
				environ[name] = value
			}
			// No ring: the plain settings move, the secrets stay where they are.
			before := newStore(t, pool, tokenseal.Keys{}, environ)
			if _, err := before.ImportEnv(ctx, env(environ), acceptAll, cli, serverconfig.ImportOptions{}); err != nil {
				t.Fatal(err)
			}
			after := newStore(t, pool, ring(t, testRing), environ)
			result, err := after.ImportEnv(ctx, env(environ), acceptAll, cli, serverconfig.ImportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Secrets) != len(tc.secret) {
				t.Fatalf("imported secrets %v, want %d", result.Secrets, len(tc.secret))
			}
			// The environment no longer holds them: they read from the database.
			loaded, err := newStore(t, pool, ring(t, testRing), nil).Current(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range tc.secret {
				if got := loaded.Getenv(name); got != value {
					t.Errorf("%s = %q, want %q", name, got, value)
				}
			}
		})
	}
}

func TestProductsAreAddOnly(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	store := newStore(t, pool, ring(t, testRing), nil)
	if err := store.AddProduct(ctx, serverconfig.ProductRow{ProductID: "old", Plan: "team", Period: "monthly", OnSale: true}, cli); err != nil {
		t.Fatal(err)
	}
	if err := store.AddProduct(ctx, serverconfig.ProductRow{ProductID: "new", Plan: "team", Period: "monthly", OnSale: true}, cli); err != nil {
		t.Fatal(err)
	}
	if err := store.AddProduct(ctx, serverconfig.ProductRow{ProductID: "new", Plan: "solo", Period: "monthly"}, cli); err == nil {
		t.Fatal("a product id was listed twice")
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The new product is sold (once billing is on); a subscription to the old one keeps its plan.
	billing := func(loaded serverconfig.Loaded) serverconfig.Snapshot {
		return loaded.Snapshot.With(map[string]string{"polar.access_token": "tok"}, nil)
	}
	if billing(loaded).Getenv("POLAR_PRODUCT_TEAM_MONTHLY") != "new" || loaded.Retired()["old"] != "team" {
		t.Fatalf("on sale %q, retired %v", billing(loaded).Getenv("POLAR_PRODUCT_TEAM_MONTHLY"), loaded.Retired())
	}
	for _, sql := range []string{`DELETE FROM polar_products WHERE product_id='old'`, `UPDATE polar_products SET plan='solo' WHERE product_id='old'`} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Errorf("%s succeeded", sql)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE operator_audit SET actor='x'`); err == nil {
		t.Error("the audit log was changed")
	}
	if err := store.SetOnSale(ctx, "old", true, cli); err != nil {
		t.Fatal(err)
	}
	loaded, _ = store.Current(ctx)
	if billing(loaded).Getenv("POLAR_PRODUCT_TEAM_MONTHLY") != "old" {
		t.Fatal("putting the old product back on sale did not take the new one off")
	}
}

func TestSnapshotReadsDefaultsAndTheTelemetryOptOut(t *testing.T) {
	empty := serverconfig.NewSnapshot(0, nil, nil, "", env(map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8", "DATABASE_URL": "postgres://x"}), true)
	if empty.Getenv("TRUSTED_PROXIES") != "" {
		t.Fatalf("unset trusted proxies = %q; want none trusted, and the environment's leftover never read", empty.Getenv("TRUSTED_PROXIES"))
	}
	if empty.Getenv("DATABASE_URL") != "postgres://x" {
		t.Fatal("bootstrap variables come from the environment")
	}
	if set := empty.With(map[string]string{"trusted_proxies": "10.0.0.5"}, nil); set.Getenv("TRUSTED_PROXIES") != "10.0.0.5" {
		t.Fatal("trusted_proxies set is not read")
	}
	off := empty.With(map[string]string{"telemetry.enabled": "false"}, nil)
	if off.Getenv("DISABLE_TELEMETRY") == "" {
		t.Fatal("telemetry turned off in the database is not off")
	}
	envOff := serverconfig.NewSnapshot(0, map[string]string{"telemetry.enabled": "true"}, nil, "", env(map[string]string{"DO_NOT_TRACK": "1"}), true)
	if envOff.Getenv("DO_NOT_TRACK") != "1" {
		t.Fatal("the environment can always turn telemetry off")
	}
}

// Every key of the registry can be stored, secrets sealed.
func TestEveryKeyCanBeSaved(t *testing.T) {
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	set := map[string]string{}
	for _, key := range serverconfig.Keys() {
		set[key.Name] = "x"
	}
	if _, err := store.Save(context.Background(), serverconfig.Change{Set: set, Source: "cli", Actor: cli}); err != nil {
		t.Fatal(err)
	}
}

// The console's history reads 100 versions in one query: 300 versions of every key stay within the
// 200 ms budget (docs/internals/performance.md).
func TestConfigHistoryStaysFast(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	set := map[string]string{}
	for _, key := range serverconfig.Keys() {
		set[key.Name] = "x"
	}
	for i := 0; i < 300; i++ {
		set["smtp.host"] = fmt.Sprintf("smtp%d.example.net", i)
		if _, err := store.Save(ctx, serverconfig.Change{Set: set, Source: "cli", Actor: cli}); err != nil {
			t.Fatal(err)
		}
	}
	best := time.Hour
	for i := 0; i < 5; i++ {
		started := time.Now()
		versions, err := store.History(ctx, 100)
		if err != nil || len(versions) != 100 || strings.Join(versions[0].Changed, ",") != "smtp.host" {
			t.Fatalf("history: %d %v", len(versions), err)
		}
		best = min(best, time.Since(started))
	}
	if best > 200*time.Millisecond {
		t.Fatalf("history of 100 versions took %s, over 200 ms", best)
	}
}
