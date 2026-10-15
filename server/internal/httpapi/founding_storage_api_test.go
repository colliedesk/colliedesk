package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// foundingCap is written out by hand: 20 GiB.
const foundingCap = 21474836480

// makeFounding puts the workspace on the Founding plan, paid until after every fixture date.
func (f limitsFixture) makeFounding(t *testing.T, workspace string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,current_period_end,source_modified_at)
		VALUES($1::uuid,'sub_founding_'||$1::text,'founding','cus_founding','active','2027-10-01T00:00:00Z','2026-10-01T09:00:00Z')`, workspace); err != nil {
		t.Fatal(err)
	}
}

// storedStorage is the maintained counter that includes imported files.
func (f limitsFixture) storedStorage(t *testing.T, workspace string) int64 {
	t.Helper()
	var stored int64
	if err := f.pool.QueryRow(context.Background(), `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`, workspace).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestFoundingStorageCapBoundaryIsExact(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.makeFounding(t, f.workspaceA)

	f.setStorage(t, f.workspaceA, foundingCap-600)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 1)); got.Code != http.StatusOK {
		t.Fatalf("mail filling the cap exactly: status=%d body=%s, want 200", got.Code, got.Body.String())
	}

	f.setStorage(t, f.workspaceA, foundingCap-599)
	before := f.counts(t, f.workspaceA)
	got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 2))
	body := got.Body.String()
	if got.Code != http.StatusForbidden || !strings.Contains(body, `"storage_limit_reached"`) {
		t.Fatalf("mail one byte over the cap: status=%d body=%s, want 403 storage_limit_reached", got.Code, body)
	}
	if !strings.Contains(body, "20 GiB") || !strings.Contains(body, "Team") {
		t.Fatalf("refusal %s must name the 20 GiB cap and say to upgrade to Team", body)
	}
	if after := f.counts(t, f.workspaceA); after != before {
		t.Fatalf("counts changed from %s to %s on a refused message", before, after)
	}
}

func TestFoundingStorageCountsImportedFiles(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.makeFounding(t, f.workspaceA)
	f.seedImported(t, f.workspaceA, conversation, foundingCap-300) // imported files count on Founding
	if got := f.storedStorage(t, f.workspaceA); got < foundingCap-300 {
		t.Fatalf("stored counter=%d, want it to include the %d imported bytes", got, foundingCap-300)
	}
	if view := f.usage(t, f.adminA); view.Usage.StorageBytes != f.storedStorage(t, f.workspaceA) {
		t.Fatalf("usage.storage_bytes=%d, want the stored counter %d", view.Usage.StorageBytes, f.storedStorage(t, f.workspaceA))
	}
	// The first mail's own stored files already use some bytes, so 600 more cannot fit.
	got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 3))
	if got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), `"storage_limit_reached"`) {
		t.Fatalf("mail after a large import: status=%d body=%s, want 403 storage_limit_reached", got.Code, got.Body.String())
	}
}

func TestFoundingStorageCounterFollowsInsertsUpdatesAndDeletes(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	base := f.storedStorage(t, f.workspaceA)
	f.seedImported(t, f.workspaceA, conversation, 1000)
	if got := f.storedStorage(t, f.workspaceA); got != base+1000 {
		t.Fatalf("after an imported insert stored=%d, want %d", got, base+1000)
	}
	if got := f.countedStorage(t, f.workspaceA); got >= base+1000 {
		t.Fatalf("counted=%d includes the imported file; the trial counter must not", got)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE attachments SET size=400 WHERE workspace_id=$1 AND size=1000`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	if got := f.storedStorage(t, f.workspaceA); got != base+400 {
		t.Fatalf("after a resize stored=%d, want %d", got, base+400)
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM attachments WHERE workspace_id=$1 AND size=400`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	if got := f.storedStorage(t, f.workspaceA); got != base {
		t.Fatalf("after a delete stored=%d, want %d", got, base)
	}
}

func TestFoundingBillingShowsTheCapAndWarningThreshold(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.makeFounding(t, f.workspaceA)
	const warn = 17179869184 // 80% of 20 GiB
	f.setStorage(t, f.workspaceA, warn)
	view := f.usage(t, f.adminA)
	if view.Plan != "founding" || view.Usage.StorageBytes != warn || view.Limits.StorageBytes == nil || *view.Limits.StorageBytes != foundingCap ||
		view.Limits.StorageWarnBytes == nil || *view.Limits.StorageWarnBytes != warn {
		t.Fatalf("billing=%+v, want founding with usage %d of cap %d, warning at %d", view, warn, foundingCap, warn)
	}
}

func TestTeamStaysUncappedWhileFoundingIsCapped(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First A")
	f.receive(t, true, 2, "First B")
	f.upgrade(t, f.workspaceA, "sub_team_uncapped")
	f.makeFounding(t, f.workspaceB)
	f.setStorage(t, f.workspaceA, foundingCap+5<<30)
	f.setStorage(t, f.workspaceB, foundingCap)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 4)); got.Code != http.StatusOK {
		t.Fatalf("team workspace at 25 GiB: status=%d body=%s, want 200", got.Code, got.Body.String())
	}
	if view := f.usage(t, f.adminA); view.Plan != "team" || view.Limits.StorageBytes != nil {
		t.Fatalf("team billing=%+v, want no storage cap", view)
	}
	if view := f.usage(t, f.adminB); view.Plan != "founding" || view.Limits.StorageBytes == nil {
		t.Fatalf("founding billing=%+v, want a storage cap", view)
	}
}

func TestTrialStorageCapIsUnchangedByTheFoundingCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.setStorage(t, f.workspaceA, storageCap-599)
	got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 5))
	if got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "2 GiB") {
		t.Fatalf("trial one byte over 2 GiB: status=%d body=%s, want 403 naming 2 GiB", got.Code, got.Body.String())
	}
	if view := f.usage(t, f.adminA); view.Limits.StorageBytes == nil || *view.Limits.StorageBytes != storageCap {
		t.Fatalf("trial limits=%+v, want the 2 GiB cap", view.Limits)
	}
}

func TestFoundingOverTheCapStillReadsRepliesAndExports(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.makeFounding(t, f.workspaceA)
	// Only the counter is raised: the export would otherwise try to read 20 GiB of seeded files that do not exist.
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=$2 WHERE id=$1`, f.workspaceA, foundingCap+1000); err != nil {
		t.Fatal(err)
	}

	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 6)); got.Code != http.StatusForbidden {
		t.Fatalf("new mail over the cap: status=%d, want 403", got.Code)
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, f.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("read over the cap: status=%d", response.Code)
	}
	note := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "Still works"}, f.adminA, nil)
	if note.Code != http.StatusCreated {
		t.Fatalf("note over the cap: status=%d body=%s, want 201", note.Code, note.Body.String())
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("export over the cap: status=%d, want 200", response.Code)
	}
	if view := f.usage(t, f.adminA); view.Plan != "founding" || view.Locked {
		t.Fatalf("billing=%+v, want an unlocked founding workspace", view)
	}
}

func TestFoundingStorageOfOneWorkspaceDoesNotCountForAnother(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First A")
	f.receive(t, true, 2, "First B")
	f.makeFounding(t, f.workspaceA)
	f.makeFounding(t, f.workspaceB)
	f.setStorage(t, f.workspaceA, foundingCap)
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 7)); got.Code != http.StatusForbidden {
		t.Fatalf("workspace A at its cap: status=%d, want 403", got.Code)
	}
	if view := f.usage(t, f.adminB); view.Usage.StorageBytes >= 1<<20 {
		t.Fatalf("workspace B usage=%d includes the bytes of A", view.Usage.StorageBytes)
	}
	if f.storedStorage(t, f.workspaceB) >= 1<<20 {
		t.Fatalf("workspace B stored counter=%d includes the bytes of A", f.storedStorage(t, f.workspaceB))
	}
	// B's own mail still fits: A's usage is not B's.
	result := f.receive(t, true, 3, "Second B")
	if result.ConversationID == nil {
		t.Fatalf("workspace B mail was not stored: %+v", result)
	}
}
