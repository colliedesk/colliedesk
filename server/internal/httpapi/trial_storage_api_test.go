package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// storageCap is written out by hand: 2 GiB.
const storageCap = 2147483648

// ownStorage is what the cap counts: attachment bytes of messages that did not come from an import.
func (f limitsFixture) ownStorage(t *testing.T, workspace string) int64 {
	t.Helper()
	var used int64
	if err := f.pool.QueryRow(context.Background(), `SELECT COALESCE(sum(a.size),0)::bigint FROM attachments a
		JOIN messages m ON m.workspace_id=a.workspace_id AND m.id=a.message_id
		WHERE a.workspace_id=$1 AND m.origin IS NULL`, workspace).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

// setStorage tops the workspace's own attachment bytes up to exactly target with one seeded row
// (no bytes are stored; only the recorded size matters to the cap). Earlier seeds are replaced.
func (f limitsFixture) setStorage(t *testing.T, workspace string, target int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM attachments WHERE workspace_id=$1 AND filename='seed.bin'`, workspace); err != nil {
		t.Fatal(err)
	}
	missing := target - f.ownStorage(t, workspace)
	if missing < 0 {
		t.Fatalf("workspace already holds more than %d bytes", target)
	}
	messageID := queryString(t, f.pool, `SELECT id::text FROM messages WHERE workspace_id=$1 AND origin IS NULL ORDER BY created_at LIMIT 1`, workspace)
	f.insertAttachment(t, workspace, messageID, missing)
}

func (f limitsFixture) insertAttachment(t *testing.T, workspace, messageID string, size int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		VALUES($1,$2,'seed.bin','application/octet-stream',$3,repeat('0',64),'seed/'||gen_random_uuid())`, workspace, messageID, size); err != nil {
		t.Fatal(err)
	}
}

// seedImported adds an imported (Freshdesk) message with one attachment of the given size.
func (f limitsFixture) seedImported(t *testing.T, workspace, conversationID string, size int64) {
	t.Helper()
	messageID := queryString(t, f.pool, `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin)
		VALUES($1,$2,'incoming','imported','{"source":"freshdesk"}') RETURNING id::text`, workspace, conversationID)
	f.insertAttachment(t, workspace, messageID, size)
}

// sizedMail is a plain customer message of exactly total bytes, no attachments.
func sizedMail(t *testing.T, total int, number int) string {
	t.Helper()
	headers := []string{"X-Original-To: support@limits-alpha." + hostedDomain, "From: sized" + strings.Repeat("x", 0) + "@customer.example",
		"To: support@limits-alpha." + hostedDomain, "Subject: Sized", "Message-ID: <sized-" + strings.Repeat("n", number) + "@customer.example>"}
	base := len(mail(headers, ""))
	if total < base {
		t.Fatalf("total %d is below the %d header bytes", total, base)
	}
	raw := mail(headers, strings.Repeat("x", total-base))
	if len(raw) != total {
		t.Fatalf("built %d bytes, want %d", len(raw), total)
	}
	return raw
}

func (f limitsFixture) counts(t *testing.T, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT (SELECT count(*) FROM conversations WHERE workspace_id=$1)||'/'||
		(SELECT count(*) FROM messages WHERE workspace_id=$1)||'/'||(SELECT count(*) FROM attachments WHERE workspace_id=$1)`, workspace)
}

func TestTrialStorageCapBoundaryIsExact(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First") // gives the workspace a message to hang seeded files on

	// Exactly at the cap: 600 bytes used up to the last byte is accepted.
	f.setStorage(t, f.workspaceA, storageCap-600)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 1)); got.Code != http.StatusOK {
		t.Fatalf("mail filling the cap exactly: status=%d body=%s, want 200", got.Code, got.Body.String())
	}

	// One byte over: refused, nothing stored.
	f.setStorage(t, f.workspaceA, storageCap-599)
	before := f.counts(t, f.workspaceA)
	got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 2))
	if got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), `"storage_limit_reached"`) {
		t.Fatalf("mail one byte over the cap: status=%d body=%s, want 403 storage_limit_reached", got.Code, got.Body.String())
	}
	if after := f.counts(t, f.workspaceA); after != before {
		t.Fatalf("counts changed from %s to %s on a refused message", before, after)
	}
}

func TestTrialStorageCapCountsOnlyOwnFiles(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.seedImported(t, f.workspaceA, conversation, 5<<30) // 5 GiB imported: not counted
	if got := f.ownStorage(t, f.workspaceA); got >= 1<<20 {
		t.Fatalf("own storage %d includes imported files", got)
	}
	f.receive(t, false, 2, "After import")
	if view := f.usage(t, f.adminA); view.Usage.StorageBytes != f.ownStorage(t, f.workspaceA) || view.Usage.StorageBytes >= 1<<20 {
		t.Fatalf("usage.storage_bytes=%d, want only own files (under 1 MiB)", view.Usage.StorageBytes)
	}
}

func TestPaidWorkspaceHasNoStorageCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.upgrade(t, f.workspaceA, "sub_storage")
	f.setStorage(t, f.workspaceA, 3<<30)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 3)); got.Code != http.StatusOK {
		t.Fatalf("paid workspace at 3 GiB: status=%d body=%s, want 200", got.Code, got.Body.String())
	}
	view := f.usage(t, f.adminA)
	if view.Limits.StorageBytes != nil || view.Limits.StorageWarnBytes != nil {
		t.Fatalf("limits=%+v, want no storage cap on a paid plan", view.Limits)
	}
}

func TestSelfHostedHasNoStorageCap(t *testing.T) {
	f := newLimitsFixture(t, false)
	f.receive(t, false, 1, "First")
	f.setStorage(t, f.workspaceA, 3<<30)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 4)); got.Code != http.StatusOK {
		t.Fatalf("self-hosted workspace at 3 GiB: status=%d body=%s, want 200", got.Code, got.Body.String())
	}
}

func TestBillingExposesStorageUsageAndWarningThreshold(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.setStorage(t, f.workspaceA, 1717986918) // 80% of 2 GiB, rounded down
	view := f.usage(t, f.adminA)
	if view.Usage.StorageBytes != 1717986918 || view.Limits.StorageBytes == nil || *view.Limits.StorageBytes != 2147483648 ||
		view.Limits.StorageWarnBytes == nil || *view.Limits.StorageWarnBytes != 1717986918 {
		t.Fatalf("billing=%+v, want usage 1717986918 of cap 2147483648, warning at 1717986918", view)
	}
	if other := f.usage(t, f.adminB); other.Usage.StorageBytes != 0 {
		t.Fatalf("workspace B usage=%d, want 0 (usage is per workspace)", other.Usage.StorageBytes)
	}
}
