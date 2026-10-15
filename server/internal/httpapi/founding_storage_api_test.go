package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
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

// checkUpload calls store.CheckStorage, the check any upload path must call. There is no agent upload
// or reply-attachment endpoint yet, so these tests prove the store function, not an HTTP route; the
// first upload route has to call it and get a test of its own. Inbound mail does not use it.
func (f limitsFixture) checkUpload(t *testing.T, workspace string, addBytes int64) error {
	t.Helper()
	ctx := context.Background()
	tx, err := database.New(f.pool).BeginWorkspace(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	return store.New(f.pool).WithPlanLimits(true).WithClock(f.clock.Now).CheckStorage(ctx, tx, workspace, addBytes, f.clock.Now())
}

func TestCheckStorageFoundingBoundaryIsExact(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First")
	f.makeFounding(t, f.workspaceA)

	f.setStorage(t, f.workspaceA, foundingCap-600)
	if err := f.checkUpload(t, f.workspaceA, 600); err != nil {
		t.Fatalf("upload filling the cap exactly: %v, want it allowed", err)
	}

	f.setStorage(t, f.workspaceA, foundingCap-599)
	err := f.checkUpload(t, f.workspaceA, 600)
	var limit *domain.StorageLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("upload one byte over the cap: %v, want a *domain.StorageLimitError", err)
	}
	if !strings.Contains(err.Error(), "20 GiB") || !strings.Contains(err.Error(), "Team") {
		t.Fatalf("refusal %q must name the 20 GiB cap and say to upgrade to Team", err)
	}
}

func TestCheckStorageRefusesFoundingAtTheCapAndNotTeam(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First A")
	f.receive(t, true, 2, "First B")
	f.makeFounding(t, f.workspaceA)
	f.upgrade(t, f.workspaceB, "sub_team_upload")
	f.setStorage(t, f.workspaceA, foundingCap)
	f.setStorage(t, f.workspaceB, foundingCap)
	var limit *domain.StorageLimitError
	if err := f.checkUpload(t, f.workspaceA, 1); !errors.As(err, &limit) {
		t.Fatalf("founding upload at the cap: %v, want a refusal", err)
	}
	if err := f.checkUpload(t, f.workspaceB, 1<<30); err != nil {
		t.Fatalf("team upload: %v, want no cap", err)
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
	// The first mail's own stored files already use some bytes, so 600 more cannot fit an upload.
	var limit *domain.StorageLimitError
	if err := f.checkUpload(t, f.workspaceA, 600); !errors.As(err, &limit) {
		t.Fatalf("upload after a large import: %v, want a refusal", err)
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

func TestFoundingOverTheCapStillTakesMailReadsRepliesAndExports(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.makeFounding(t, f.workspaceA)
	// Only the counter is raised: the export would otherwise try to read 20 GiB of seeded files that do not exist.
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=$2 WHERE id=$1`, f.workspaceA, foundingCap+1000); err != nil {
		t.Fatal(err)
	}

	// A customer's email is never bounced for storage: it is stored in full, attachments included.
	result := inboundOK(t, f.api, f.secretA, mailWithFiles("over-the-cap@customer.example"))
	if result.Status != "created" || result.ConversationID == nil {
		t.Fatalf("mail with attachments over the cap: %+v, want created", result)
	}
	var files int
	for _, message := range storedMessages(t, f.api, f.adminA, *result.ConversationID) {
		files += len(message.Attachments)
	}
	if files < 2 {
		t.Fatalf("stored %d attachments, want the PDF and the inline image kept", files)
	}
	if got := inbound(t, f.api, platformSecret, sizedMail(t, 600, 6)); got.Code != http.StatusOK {
		t.Fatalf("plain mail over the cap: status=%d body=%s, want 200", got.Code, got.Body.String())
	}

	// The usage still shows it, so the billing page can warn.
	view := f.usage(t, f.adminA)
	if view.Plan != "founding" || view.Locked || view.Limits.StorageBytes == nil || view.Usage.StorageBytes <= *view.Limits.StorageBytes {
		t.Fatalf("billing=%+v, want an unlocked founding workspace whose usage is past the cap", view)
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
}

func TestFoundingStorageOfOneWorkspaceDoesNotCountForAnother(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "First A")
	f.receive(t, true, 2, "First B")
	f.makeFounding(t, f.workspaceA)
	f.makeFounding(t, f.workspaceB)
	f.setStorage(t, f.workspaceA, foundingCap)
	var limit *domain.StorageLimitError
	if err := f.checkUpload(t, f.workspaceA, 600); !errors.As(err, &limit) {
		t.Fatalf("workspace A at its cap: %v, want a refusal", err)
	}
	if view := f.usage(t, f.adminB); view.Usage.StorageBytes >= 1<<20 {
		t.Fatalf("workspace B usage=%d includes the bytes of A", view.Usage.StorageBytes)
	}
	if f.storedStorage(t, f.workspaceB) >= 1<<20 {
		t.Fatalf("workspace B stored counter=%d includes the bytes of A", f.storedStorage(t, f.workspaceB))
	}
	// B's own upload still fits: A's usage is not B's.
	if err := f.checkUpload(t, f.workspaceB, 600); err != nil {
		t.Fatalf("workspace B upload: %v, want it allowed", err)
	}
}
