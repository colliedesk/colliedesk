package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHelpAndHealthcheckDoNotRequireDatabase(t *testing.T) {
	var help bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &help, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help.String(), "create-workspace") {
		t.Fatalf("help omitted create-workspace: %q", help.String())
	}

	healthy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer healthy.Close()
	if err := run(context.Background(), []string{"healthcheck", "--url", healthy.URL}, io.Discard, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"healthcheck", "--url", healthy.URL}, io.Discard, "://invalid", ""); err == nil || !strings.Contains(err.Error(), "healthcheck database") {
		t.Fatalf("healthcheck accepted invalid database URL: %v", err)
	}
}

func TestCreateWorkspaceAndPairPrintFreshOneTimeURLs(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL CLI test")
	}
	databaseURL := isolatedDatabaseURL(t, baseURL)
	var first, hint bytes.Buffer
	stderr = &hint
	t.Cleanup(func() { stderr = os.Stderr })
	err := run(context.Background(), []string{"create-workspace", "--name", "CLI Desk", "--admin-email", "Admin@CLI.example.com"}, &first, databaseURL, "https://desk.example.com")
	if err != nil {
		t.Fatal(err)
	}
	firstURL := strings.TrimSpace(first.String())
	if strings.Count(first.String(), "\n") != 1 {
		t.Fatalf("stdout must hold only the link (install.sh captures it): %q", first.String())
	}
	// A self-hoster whose link expired cannot sign in to look up the slug, so creation tells them.
	if want := "colliedesk pair --workspace cli-desk --email admin@cli.example.com"; !strings.Contains(hint.String(), want) {
		t.Fatalf("create-workspace hint %q does not contain %q", hint.String(), want)
	}
	parsed, err := url.Parse(firstURL)
	if err != nil || parsed.Query().Get("token") == "" || parsed.Path != "/redeem" {
		t.Fatalf("invalid pairing URL %q: %v", firstURL, err)
	}

	var second bytes.Buffer
	err = run(context.Background(), []string{"pair", "--workspace", "cli-desk", "--email", "admin@cli.example.com"}, &second, databaseURL, "https://desk.example.com")
	if err != nil {
		t.Fatal(err)
	}
	secondURL := strings.TrimSpace(second.String())
	if secondURL == firstURL {
		t.Fatal("pair must print a fresh one-time URL")
	}
}

func isolatedDatabaseURL(t *testing.T, baseURL string) string {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_cli_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
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
