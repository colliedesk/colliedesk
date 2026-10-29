package serverconfig_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

var operator = serverconfig.Actor{Kind: "operator", Name: "ops@example.com"}

// D12: a change that can lock everyone out (trusted proxies, cloud sign-in, the help center domain) is tried
// for a while; unless confirmed, it is reverted by itself, and nothing else is saved meanwhile.
func TestATrialRevertsByItselfUnlessConfirmed(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	store := newStore(t, pool, ring(t, testRing), nil)
	before, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"trusted_proxies": "10.0.0.0/8"}, Source: "cli", Actor: cli})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := store.Save(ctx, serverconfig.Change{Base: before, CheckBase: true, Set: map[string]string{"trusted_proxies": "192.168.0.0/16"}, Source: "console", Actor: operator, Trial: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	pending, ok, err := store.Trial(ctx)
	if err != nil || !ok || pending.Version != trial || pending.Parent != before || pending.ExpiresAt.IsZero() {
		t.Fatalf("trial = %+v %v %v, want v%d on v%d with an end", pending, ok, err, trial, before)
	}
	// The console and approved requests wait for the trial.
	for _, source := range []string{"console", "request"} {
		if _, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"smtp.host": "smtp.example.net"}, Source: source, Actor: operator}); !errors.Is(err, serverconfig.ErrTrialPending) {
			t.Fatalf("%s save during a trial: %v, want ErrTrialPending", source, err)
		}
	}
	if _, err := store.Restore(ctx, before, 0, operator, ""); !errors.Is(err, serverconfig.ErrTrialPending) {
		t.Fatalf("restore from the console during a trial: %v, want ErrTrialPending", err)
	}
	// Not yet expired: nothing happens.
	if reverted, err := store.RevertExpiredTrial(ctx); err != nil || reverted != 0 {
		t.Fatalf("early revert = %d %v", reverted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE server_config_versions SET trial_expires_at = now() - interval '1 second' WHERE id=$1`, trial); err != nil {
		t.Fatal(err)
	}
	reverted, err := store.RevertExpiredTrial(ctx)
	if err != nil || reverted == 0 {
		t.Fatalf("revert = %d %v", reverted, err)
	}
	loaded, err := store.Current(ctx)
	if err != nil || loaded.Version != reverted || loaded.Getenv("TRUSTED_PROXIES") != "10.0.0.0/8" {
		t.Fatalf("after the revert v%d proxies %q (%v), want v%d with the value before", loaded.Version, loaded.Getenv("TRUSTED_PROXIES"), err, reverted)
	}
	history, err := store.History(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if history[0].Source != "trial-auto-revert" || history[1].ID != trial || history[1].Status != "reverted" {
		t.Fatalf("history = %+v, want the auto revert on top of the reverted trial", history[:2])
	}
	if _, ok, _ := store.Trial(ctx); ok {
		t.Fatal("a trial still runs after its revert")
	}
	entries, err := store.AuditLog(ctx, "", "", time.Time{}, 5)
	if err != nil || entries[0].Action != "trial.auto_revert" {
		t.Fatalf("audit = %+v %v, want trial.auto_revert first", entries, err)
	}
	if again, err := store.RevertExpiredTrial(ctx); err != nil || again != 0 {
		t.Fatalf("second revert = %d %v, want nothing", again, err)
	}
}

func TestATrialConfirmedStaysAndARevertedOneComesBack(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, databasetest.Pool(t), ring(t, testRing), nil)
	trial, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"help_center.domain": "help.example.com"}, Source: "console", Actor: operator, Trial: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmTrial(ctx, trial, operator); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmTrial(ctx, trial, operator); !errors.Is(err, serverconfig.ErrNoTrial) {
		t.Fatalf("second confirm: %v, want ErrNoTrial", err)
	}
	loaded, _ := store.Current(ctx)
	if loaded.Version != trial || loaded.Getenv("HELP_CENTER_DOMAIN") != "help.example.com" {
		t.Fatalf("after confirm v%d, want v%d", loaded.Version, trial)
	}
	second, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"help_center.domain": "kb.example.com"}, Source: "console", Actor: operator, Trial: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevertTrial(ctx, trial, operator); !errors.Is(err, serverconfig.ErrNoTrial) {
		t.Fatalf("revert of another version: %v, want ErrNoTrial", err)
	}
	back, err := store.RevertTrial(ctx, second, operator)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ = store.Current(ctx)
	if loaded.Version != back || loaded.Getenv("HELP_CENTER_DOMAIN") != "help.example.com" {
		t.Fatalf("after revert v%d %q, want v%d with the confirmed domain", loaded.Version, loaded.Getenv("HELP_CENTER_DOMAIN"), back)
	}
}

// D12: the shell is always the way back. A save, reset or restore from the CLI during a trial ends it (the
// audit says so) instead of waiting, and nothing reverts afterwards.
func TestTheShellEndsATrialInsteadOfWaiting(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	store := newStore(t, pool, ring(t, testRing), nil)
	before, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"trusted_proxies": "10.0.0.0/8"}, Source: "cli", Actor: cli})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"trusted_proxies": "192.168.0.0/16"}, Source: "console", Actor: operator, Trial: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	reset, err := store.Save(ctx, serverconfig.Change{Unset: []string{"trusted_proxies"}, Source: "cli", Actor: cli})
	if err != nil {
		t.Fatalf("collie config reset during a trial: %v", err)
	}
	if _, ok, _ := store.Trial(ctx); ok {
		t.Fatal("the trial still runs after the shell's reset")
	}
	if _, err := pool.Exec(ctx, `UPDATE server_config_versions SET trial_expires_at = now() - interval '1 second' WHERE id=$1`, trial); err != nil {
		t.Fatal(err)
	}
	if reverted, err := store.RevertExpiredTrial(ctx); err != nil || reverted != 0 {
		t.Fatalf("a superseded trial reverted later: %d %v", reverted, err)
	}
	loaded, _ := store.Current(ctx)
	if loaded.Version != reset || loaded.Getenv("TRUSTED_PROXIES") != "" {
		t.Fatalf("after reset v%d proxies %q, want v%d with none", loaded.Version, loaded.Getenv("TRUSTED_PROXIES"), reset)
	}
	entries, _ := store.AuditLog(ctx, "", "", time.Time{}, 5)
	found := false
	for _, entry := range entries {
		found = found || (entry.Action == "trial.superseded" && entry.VersionID != nil && *entry.VersionID == trial)
	}
	if !found {
		t.Fatalf("no trial.superseded line for v%d: %+v", trial, entries)
	}
	// A restore from the shell ends a trial too.
	second, err := store.Save(ctx, serverconfig.Change{Set: map[string]string{"trusted_proxies": "172.16.0.0/12"}, Source: "console", Actor: operator, Trial: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore(ctx, before, 0, cli, ""); err != nil {
		t.Fatalf("collie config restore during a trial: %v", err)
	}
	history, _ := store.History(ctx, 5)
	for _, version := range history {
		if version.ID == second && version.Status != "reverted" {
			t.Fatalf("the trial v%d restored over is %q, want reverted", second, version.Status)
		}
	}
}
