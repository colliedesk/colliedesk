package freshdesk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func TestNextPageLinkToAnotherHostIsRefused(t *testing.T) {
	env := newImportEnv(t)
	env.fake.foreignNextLink = true
	_, err := env.run(t, context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "next page") {
		t.Fatalf("error = %v, want refusal of a foreign next-page link", err)
	}
	if env.fake.fileAuthSeen || env.fake.count("/files/api/v2/agents") != 0 {
		t.Fatal("the importer followed a next-page link to another host")
	}
}

func TestAttachmentRedirectToPrivateAddressIsBlocked(t *testing.T) {
	env := newImportEnv(t)
	env.fake.redirectFile = "9003"
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || !hasFailure(report, "attachment", "9003", "2") {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if env.fake.count("/internal/latest/meta-data") != 0 {
		t.Fatal("a redirect reached a loopback address")
	}
}

func TestPlainHTTPAccountURLIsRefused(t *testing.T) {
	env := newImportEnv(t)
	_, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
		o.BaseURL = strings.Replace(env.fake.api.URL, "https://", "http://", 1)
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("error = %v, want https required", err)
	}
	if env.fake.count("/api/v2/account") != 0 {
		t.Fatal("a request went out over plain HTTP")
	}
}

func TestStalledAPICallTimesOutAndIsRetried(t *testing.T) {
	env := newImportEnv(t)
	env.fake.stallPath = "/api/v2/companies"
	started := time.Now()
	_, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
		freshdesk.SetTestTimeouts(o, 200*time.Millisecond, time.Second)
	})
	if err == nil || time.Since(started) > 10*time.Second {
		t.Fatalf("error = %v after %s, want a quick timeout", err, time.Since(started))
	}
	if got := env.fake.count("/api/v2/companies"); got != 5 {
		t.Fatalf("companies requests = %d, want 1 + 4 retries", got)
	}
}

func TestStalledDownloadFailsOnlyThatAttachment(t *testing.T) {
	env := newImportEnv(t)
	env.fake.stallFile = "9003"
	started := time.Now()
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
		freshdesk.SetTestTimeouts(o, 5*time.Second, 300*time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != freshdesk.StatusFailed || !hasFailure(report, "attachment", "9003", "2") || report.Attachments.Imported != 2 {
		t.Fatalf("status=%s attachments=%+v failures=%+v", report.Status, report.Attachments, report.Failures)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("stalled download held the run for %s", time.Since(started))
	}
}
