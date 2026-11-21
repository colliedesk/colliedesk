package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

func analyticsStore(t *testing.T, d analyticsDesk) *store.Store {
	t.Helper()
	keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	return store.New(d.pool).WithSealKeys(keys)
}

// A send to the settings a team has since saved over never shows on the new settings.
func TestASendResultOfSettingsSavedOverIsNotShownOnTheNewOnes(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-version", nil, hcanalytics.Options{FlushEvery: time.Hour})
	ctx, st := context.Background(), analyticsStore(t, d)
	workspace := workspaceID(t, d.api, d.admin)
	d.save(t, ga4Settings(true), http.StatusOK)
	before, err := st.HelpCenterAnalyticsTarget(ctx, workspace)
	if err != nil || before == nil {
		t.Fatalf("target = %v, %v", before, err)
	}
	d.save(t, map[string]any{"provider": "plausible", "enabled": true, "plausible_domain": "help.acme.example"}, http.StatusOK)
	sentAt := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	stale := hcanalytics.Result{WorkspaceID: workspace, Version: before.Version, SentAt: sentAt, Error: "GA4 refused it.", ErrorAt: sentAt}
	if err := st.RecordHelpCenterAnalyticsResults(ctx, []hcanalytics.Result{stale}); err != nil {
		t.Fatal(err)
	}
	if got := d.settings(t, d.admin); got["last_sent_at"] != nil || got["last_error"] != nil {
		t.Fatalf("settings = %v, want no send result: the one recorded was for the GA4 settings saved over", got)
	}
	after, err := st.HelpCenterAnalyticsTarget(ctx, workspace)
	if err != nil || after == nil {
		t.Fatalf("target = %v, %v", after, err)
	}
	current := hcanalytics.Result{WorkspaceID: workspace, Version: after.Version, SentAt: sentAt}
	if err := st.RecordHelpCenterAnalyticsResults(ctx, []hcanalytics.Result{current}); err != nil {
		t.Fatal(err)
	}
	if got := d.settings(t, d.admin); got["last_sent_at"] != "2026-11-01T10:00:00Z" {
		t.Fatalf("last_sent_at = %v, want the send to the settings saved now", got["last_sent_at"])
	}
}

// A saved API secret this server cannot open (its key was removed from MAILBOX_TOKEN_KEYS) says
// what to do on the test, and the help center keeps sending for the other workspaces.
func TestAnAPISecretThisServerCannotOpenSaysWhatToDo(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-unopenable", nil, hcanalytics.Options{FlushEvery: time.Hour})
	ctx := context.Background()
	workspace := workspaceID(t, d.api, d.admin)
	d.save(t, ga4Settings(true), http.StatusOK)
	if _, err := d.pool.Exec(ctx, `UPDATE help_center_analytics SET ga4_api_secret_key_id='k-removed' WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
	response := kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/analytics/test", nil, d.admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "analytics_secret_unreadable" {
		t.Fatalf("code = %q, want analytics_secret_unreadable", code)
	}
	targets, err := analyticsStore(t, d).HelpCenterAnalyticsTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.WorkspaceID == workspace {
			if target.Problem != "The saved GA4 API secret cannot be opened on this server: enter it again and save." || target.APISecret != "" {
				t.Fatalf("target = %+v, want its problem and no secret", target)
			}
			return
		}
	}
	t.Fatalf("targets = %+v, want the unreadable one listed with its problem", targets)
}
