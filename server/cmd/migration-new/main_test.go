package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationNewCreatesPairedDomainNamedFiles(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "server", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time {
		return time.Date(2026, 10, 14, 6, 10, 11, 0, time.UTC)
	}
	var output bytes.Buffer

	err := run([]string{"--repo", repo, "--name", "core-helpdesk"}, &output, now)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, direction := range []string{"up", "down"} {
		name := "20261014061011-core-helpdesk." + direction + ".sql"
		if _, err := os.Stat(filepath.Join(repo, "server", "migrations", name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

func TestMigrationNewRejectsNonKebabName(t *testing.T) {
	var output bytes.Buffer
	err := run([]string{"--repo", t.TempDir(), "--name", "Core Helpdesk"}, &output, time.Now)
	if err == nil {
		t.Fatal("expected invalid name error")
	}
}
