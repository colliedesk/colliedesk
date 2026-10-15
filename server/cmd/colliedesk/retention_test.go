package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/retention"
)

func TestRetentionRefusesOnAnInstanceWithoutBilling(t *testing.T) {
	env := newTrialEnv(t, "Old Shop")
	env.mustRun("trial", "set", "--workspace", "old-shop", "--ends", "2020-01-01T00:00:00Z")
	for _, mode := range []string{"plan", "run"} {
		output, err := env.run("retention", mode)
		if err == nil || !strings.Contains(err.Error(), "hosted cloud only") || output != "" {
			t.Fatalf("retention %s on self-hosted: output %q err %v; want a refusal", mode, output, err)
		}
	}
	var found bool
	if err := env.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM workspaces WHERE slug='old-shop')`).Scan(&found); err != nil || !found {
		t.Fatalf("self-hosted workspace gone or unreadable: %v %v", found, err)
	}
	if _, err := env.run("retention"); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("retention without a mode: %v, want usage", err)
	}
}

func TestRetentionPlanListsDueWorkspacesAndChangesNothing(t *testing.T) {
	env := newTrialEnv(t, "Old Shop")
	env.mustRun("trial", "set", "--workspace", "old-shop", "--ends", "2026-11-01T00:00:00Z")
	service := retention.New(retention.Config{DB: database.New(env.pool), Now: func() time.Time { return time.Date(2027, 10, 2, 0, 0, 0, 0, time.UTC) }})
	var output bytes.Buffer
	if err := runRetention(context.Background(), service, false, &output); err != nil {
		t.Fatal(err)
	}
	want := "notice\told-shop\t" + env.workspaceID + "\tlocked_since=2026-11-01T00:00:00Z\tnotice_at=2027-10-02T00:00:00Z\tdelete_at=2027-11-01T00:00:00Z\n"
	if !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "workspace(s) to act on") {
		t.Fatalf("plan output:\n%s\nwant a line %q", output.String(), want)
	}
	if strings.Contains(output.String(), "@") {
		t.Fatalf("plan output must not carry email addresses:\n%s", output.String())
	}
}
