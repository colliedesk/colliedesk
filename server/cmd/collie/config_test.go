package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runConfig runs collie config with args and stdin, returning stdout and the error.
func runConfig(t *testing.T, databaseURL, input string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("USER", "tests")
	previous := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = previous }()
	var output bytes.Buffer
	err := run(context.Background(), append([]string{"config"}, args...), &output, databaseURL, trialTestOrigin)
	return output.String(), err
}

func mustConfig(t *testing.T, databaseURL, input string, args ...string) string {
	t.Helper()
	output, err := runConfig(t, databaseURL, input, args...)
	if err != nil {
		t.Fatalf("collie config %v: %v\n%s", args, err, output)
	}
	return output
}

func TestConfigSetGetHistoryAndRestore(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	mustConfig(t, databaseURL, "", "set", "SMTP_HOST=smtp.example.net", "smtp.port=2525")
	if got := mustConfig(t, databaseURL, "", "get", "smtp.host"); got != "smtp.host=smtp.example.net\n" {
		t.Fatalf("get = %q", got)
	}
	mustConfig(t, databaseURL, "", "set", "smtp.host=other.example.net")
	history := mustConfig(t, databaseURL, "", "history")
	if !strings.HasPrefix(history, "v2\t") || !strings.Contains(history, "cli:tests") || !strings.Contains(history, "smtp.host,smtp.port") {
		t.Fatalf("history = %q", history)
	}
	mustConfig(t, databaseURL, "", "restore", "v1")
	if got := mustConfig(t, databaseURL, "", "get", "SMTP_HOST"); got != "smtp.host=smtp.example.net\n" {
		t.Fatalf("after restore get = %q", got)
	}
	mustConfig(t, databaseURL, "", "reset", "smtp.port")
	if list := mustConfig(t, databaseURL, "", "list", "--group", "email"); !strings.Contains(list, "smtp.port") || !strings.Contains(list, "587\tdefault") {
		t.Fatalf("list = %q", list)
	}
}

func TestConfigRefusesWhatServeWouldRefuse(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	for _, args := range [][]string{
		{"set", "smtp.port=465"},
		{"set", "smtp.hots=x"},
		{"set", "smtp.password=x"},
		{"set", "retention.max_deletions_per_day=50"},
		{"set", "retention.delete=on"}, // a guarded setting needs --yes
	} {
		if _, err := runConfig(t, databaseURL, "", args...); err == nil {
			t.Errorf("%v: saved", args)
		}
	}
	if history := mustConfig(t, databaseURL, "", "history"); history != "" {
		t.Fatalf("a refused change was saved: %q", history)
	}
	// Billing set in part is refused too, and the error names settings, never the secret.
	const canary = "polar_oat_canary-never-printed"
	_, err := runConfig(t, databaseURL, canary, "set-secret", "polar.access_token")
	if err == nil || strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "POLAR_PRODUCT_TEAM_MONTHLY") {
		t.Fatalf("partial billing: %v", err)
	}
}

func TestConfigSecretsAreWriteOnly(t *testing.T) {
	databaseURL := cliDatabase(t)
	const canary = "smtp-secret-canary-41"
	if _, err := runConfig(t, databaseURL, canary, "set-secret", "smtp.password"); err == nil || !strings.Contains(err.Error(), "MAILBOX_TOKEN_KEYS") {
		t.Fatalf("without a key ring: %v", err)
	}
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	mustConfig(t, databaseURL, "", "set", "smtp.host=smtp.example.net")
	mustConfig(t, databaseURL, canary+"\n", "set-secret", "smtp.password")
	if got := mustConfig(t, databaseURL, "", "get", "smtp.password"); got != "smtp.password=(set)\n" {
		t.Fatalf("get = %q", got)
	}
	for _, args := range [][]string{{"export"}, {"export", "--format", "json"}, {"list"}, {"history"}} {
		if output := mustConfig(t, databaseURL, "", args...); strings.Contains(output, canary) {
			t.Fatalf("%v printed the secret", args)
		}
	}
	var warned bytes.Buffer
	previous := stderr
	stderr = &warned
	defer func() { stderr = previous }()
	if output := mustConfig(t, databaseURL, "", "export", "--include-secrets"); !strings.Contains(output, "SMTP_PASSWORD="+canary) {
		t.Fatalf("export --include-secrets = %q", output)
	}
	if !strings.Contains(warned.String(), "plain text") {
		t.Fatalf("no warning on stderr: %q", warned.String())
	}
}

func TestConfigExportImportsBackAndApplyIsIdempotent(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	mustConfig(t, databaseURL, "", "set", "smtp.host=smtp.example.net", "internal_signup.emails=@hclabs.co")
	exported := mustConfig(t, databaseURL, "", "export", "--format", "json")
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte(exported), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustConfig(t, databaseURL, "", "apply", file); !strings.Contains(got, "already matches") {
		t.Fatalf("apply of the export = %q", got)
	}
	mustConfig(t, databaseURL, "", "set", "smtp.host=drift.example.net", "mail_image_cache.max_bytes=100")
	if dry := mustConfig(t, databaseURL, "", "apply", file, "--dry-run"); !strings.Contains(dry, "~ smtp.host=smtp.example.net") || !strings.Contains(dry, "- mail_image_cache.max_bytes") {
		t.Fatalf("dry run = %q", dry)
	}
	if got := mustConfig(t, databaseURL, "", "get", "smtp.host"); got != "smtp.host=drift.example.net\n" {
		t.Fatal("a dry run saved")
	}
	mustConfig(t, databaseURL, "", "apply", file)
	if got := mustConfig(t, databaseURL, "", "get", "smtp.host"); got != "smtp.host=smtp.example.net\n" {
		t.Fatalf("after apply %q", got)
	}

	other := cliDatabase(t)
	envFile := filepath.Join(t.TempDir(), "rollback.env")
	if err := os.WriteFile(envFile, []byte(mustConfig(t, databaseURL, "", "export")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustConfig(t, other, "", "import", envFile); !strings.Contains(got, "imported email") || !strings.Contains(got, "imported internal-signup") {
		t.Fatalf("import of the env export = %q", got)
	}
	if got := mustConfig(t, other, "", "get", "INTERNAL_SIGNUP_EMAILS"); got != "internal_signup.emails=@hclabs.co\n" {
		t.Fatalf("imported %q", got)
	}
}

func TestConfigDoctorExitCodes(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	for _, name := range append(serverconfig.MovedEnvs(), serverconfig.DeadEnvs...) {
		t.Setenv(name, "")
	}
	if got := mustConfig(t, databaseURL, "", "doctor"); !strings.Contains(got, "clean") {
		t.Fatalf("doctor = %q", got)
	}
	mustConfig(t, databaseURL, "", "set", "smtp.host=smtp.example.net")
	t.Setenv("SMTP_HOST", "smtp.example.net")
	t.Setenv("APP_SECRET", "left-over")
	output, err := runConfig(t, databaseURL, "", "doctor")
	var code exitCode
	if !errors.As(err, &code) || code.code != 1 || !strings.Contains(output, "SMTP_HOST is still set") || !strings.Contains(output, "APP_SECRET") {
		t.Fatalf("doctor with leftovers: %v %q", err, output)
	}
	if strings.Contains(output, "left-over") {
		t.Fatal("doctor printed a value")
	}
	pool := cliPool(t, databaseURL)
	store, _ := openConfigStore(database.New(pool))
	loaded, _ := store.Current(context.Background())
	if err := store.Reject(context.Background(), loaded.Version, "smtp.port: not valid", serverconfig.Actor{Kind: "server", Name: "serve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runConfig(t, databaseURL, "", "doctor"); !errors.As(err, &code) || code.code != 2 {
		t.Fatalf("doctor with a refused version: %v", err)
	}
}

func cliPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := openPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// A version that does not build never stops the server: it is refused, and the last good one runs.
func TestStartRunsTheLastGoodVersionWhenTheNewestDoesNotBuild(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	for _, name := range serverconfig.MovedEnvs() {
		t.Setenv(name, "")
	}
	pool := cliPool(t, databaseURL)
	store, err := openConfigStore(database.New(pool))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	good, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"smtp.host": "good.example.net"}, Source: "cli", Actor: serverconfig.Actor{Name: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := startConfig(ctx, store, quiet); err != nil {
		t.Fatal(err)
	}
	// Written past the checks, as an older binary or a hand edit could.
	const canary = "polar_oat_never-printed"
	bad, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"smtp.host": "bad.example.net", "polar.access_token": canary}, Source: "cli", Actor: serverconfig.Actor{Name: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	built, _, err := startConfig(ctx, store, slog.New(slog.NewTextHandler(&logged, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if built.Email.SMTP.Host != "good.example.net" || built.Billing.Enabled() {
		t.Fatalf("started with %q billing=%v, want v%d", built.Email.SMTP.Host, built.Billing.Enabled(), good)
	}
	history, _ := store.History(ctx, 5)
	if history[0].ID != bad || history[0].Status != "rejected" || !strings.Contains(history[0].Rejected, "POLAR_PRODUCT_TEAM_MONTHLY") {
		t.Fatalf("history = %+v", history[0])
	}
	if strings.Contains(logged.String(), canary) || strings.Contains(history[0].Rejected, canary) {
		t.Fatal("a refused version's error carries a secret")
	}
}

// Every secret a parser could quote in an error is replaced by its name.
func TestValidationErrorsNeverCarryASecret(t *testing.T) {
	for _, key := range serverconfig.Keys() {
		if !key.Secret {
			continue
		}
		canary := "canary-" + strings.ReplaceAll(key.Name, ".", "-")
		snap := serverconfig.NewSnapshot(1, map[string]string{key.Name: canary}, nil, "", nil, true)
		err := redactSecrets(fmt.Errorf("%s must be something else, got %q", key.Env, canary), snap)
		if strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), key.Name) {
			t.Errorf("%s: %v", key.Name, err)
		}
	}
}

// Nothing in cmd/collie reads a moved setting from the environment: only bootstrap variables, the
// telemetry opt-out and the CLI's own inputs.
func TestServerReadsOnlyBootstrapVariablesFromTheEnvironment(t *testing.T) {
	allowed := map[string]bool{"USER": true}
	for _, name := range append(serverconfig.Bootstrap, serverconfig.TelemetryOffEnvs...) {
		allowed[name] = true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || strings.HasPrefix(file, "e2e_") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			_, after, found := strings.Cut(line, `os.Getenv("`)
			if !found {
				continue
			}
			name, _, _ := strings.Cut(after, `"`)
			if !allowed[name] {
				t.Errorf("%s reads %s from the environment; settings come from the database (serverconfig)", file, name)
			}
		}
	}
	for _, key := range serverconfig.Keys() {
		if key.Env != "" && allowed[key.Env] {
			t.Errorf("%s is both a bootstrap variable and a setting", key.Env)
		}
	}
}

// install.sh moves the settings an older .env holds into the database; its list is the server's.
func TestInstallerImportsEverySettingTheServerMoved(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, found := strings.Cut(string(raw), "MOVED_SETTINGS='")
	list, _, closed := strings.Cut(after, "'")
	if !found || !closed {
		t.Fatal("install.sh has no MOVED_SETTINGS list")
	}
	got := strings.Fields(list)
	want := serverconfig.MovedEnvs()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("install.sh MOVED_SETTINGS = %v\nwant %v", got, want)
	}
}

// Without a key ring secrets are read from the environment (D8), so the shipped compose files must
// still pass every secret setting's variable to the server.
func TestShippedComposeFilesPassEverySecretForAServerWithoutAKeyRing(t *testing.T) {
	for _, file := range []string{"compose.yaml", "install.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range serverconfig.Keys() {
			if key.Secret && !strings.Contains(string(raw), key.Env+": ${"+key.Env+":-}") {
				t.Errorf("%s does not pass %s to the server", file, key.Env)
			}
		}
	}
}

// An imported file without SIGNUP_HASH_KEY never makes one: the server's environment may still hold the
// key, and it is imported at the server's start instead of a generated one.
func TestConfigImportOfAFileNeverGeneratesTheSignupKey(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	mustConfig(t, databaseURL, "SMTP_HOST=smtp.example.net\n", "import", "--stdin")
	pool := cliPool(t, databaseURL)
	var keys int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM server_secrets WHERE name='signup-hash-key'`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 0 {
		t.Fatal("a file import generated the sign-up hash key")
	}
	if got := mustConfig(t, databaseURL, "SIGNUP_HASH_KEY=from-the-file-0123456789abcdef0123456789\n", "import", "--stdin"); !strings.Contains(got, "sign-up hash key imported") {
		t.Fatalf("import with the key = %q", got)
	}
}
