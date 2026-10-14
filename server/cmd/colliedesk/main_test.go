package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateWorkspaceAndPairPrintFreshOneTimeURLs(t *testing.T) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL CLI test")
	}
	databaseURL := isolatedDatabaseURL(t, baseURL)
	var first bytes.Buffer
	err := run(context.Background(), []string{"create-workspace", "--name", "CLI Desk", "--admin-email", "admin@cli.example.com"}, &first, databaseURL, "https://desk.example.com")
	if err != nil {
		t.Fatal(err)
	}
	firstURL := strings.TrimSpace(first.String())
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
