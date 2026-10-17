package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type replyPersonView struct {
	AgentID *string `json:"agent_id"`
	Name    string  `json:"name"`
}

type replyChangeView struct {
	Version             int              `json:"version"`
	Kind                string           `json:"kind"`
	RestoredFromVersion *int             `json:"restored_from_version"`
	ChangedBy           *replyPersonView `json:"changed_by"`
	Token               *struct {
		ID    *string `json:"id"`
		Label string  `json:"label"`
	} `json:"token"`
	AgentRequestID *string `json:"agent_request_id"`
	CreatedAt      string  `json:"created_at"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
}

type replyWithChangeView struct {
	savedReplyView
	LastChange *replyChangeView `json:"last_change"`
}

type trashedReplyView struct {
	replyWithChangeView
	DeletedAt time.Time        `json:"deleted_at"`
	DeletedBy *replyPersonView `json:"deleted_by"`
	PurgesAt  time.Time        `json:"purges_at"`
}

func replyVersions(t *testing.T, api http.Handler, token, id string) []replyChangeView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/saved-replies/"+id+"/versions", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("versions of %s status=%d body=%s", id, response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/saved-replies/"+id+"/versions", nil, token, nil), response)
	var page struct {
		Items []replyChangeView `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func replyTrash(t *testing.T, api http.Handler, token string) []trashedReplyView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/saved-replies/trash", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("trash status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/saved-replies/trash", nil, token, nil), response)
	var page struct {
		Items []trashedReplyView `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func listRepliesWithChanges(t *testing.T, api http.Handler, token string) []replyWithChangeView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/saved-replies", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list saved replies status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []replyWithChangeView `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func versionKinds(versions []replyChangeView) []string {
	kinds := make([]string, len(versions))
	for index, version := range versions {
		kinds[index] = strconv.Itoa(version.Version) + ":" + version.Kind
	}
	return kinds
}

func TestSendTokenWritesSavedRepliesDirectlyAndEveryChangeIsAVersion(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "History Desk", "admin@history.example.com")
	token := issueToken(t, api, admin, "Reply writer", "send").Token

	created := request(t, api, http.MethodPost, "/api/v1/saved-replies", map[string]any{"title": "Refund", "body": "We refunded you."}, token, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("send token create status=%d body=%s, want 201", created.Code, created.Body.String())
	}
	var reply savedReplyView
	decode(t, created, &reply)
	if edited := patchReply(t, api, token, reply.ID, map[string]any{"body": "We refunded you today."}); edited.Code != http.StatusOK {
		t.Fatalf("send token edit status=%d body=%s", edited.Code, edited.Body.String())
	}

	listed := listRepliesWithChanges(t, api, admin)
	if len(listed) != 1 || listed[0].LastChange == nil {
		t.Fatalf("list = %+v, want one reply with its last change", listed)
	}
	last := listed[0].LastChange
	if last.Version != 2 || last.Kind != "edit" || last.Token == nil || last.Token.Label != "Reply writer" || last.ChangedBy == nil || last.ChangedBy.Name == "" {
		t.Fatalf("last change = %+v, want version 2 edit by the token's owner via Reply writer", last)
	}

	if deleted := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+reply.ID, nil, token, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("send token delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if remaining := listSavedReplies(t, api, admin); len(remaining) != 0 {
		t.Fatalf("a deleted reply is still listed: %+v", remaining)
	}
	trash := replyTrash(t, api, admin)
	if len(trash) != 1 || trash[0].ID != reply.ID || trash[0].Body != "We refunded you today." || trash[0].DeletedBy == nil {
		t.Fatalf("trash = %+v, want the deleted reply with who deleted it", trash)
	}
	if days := trash[0].PurgesAt.Sub(trash[0].DeletedAt); days != 30*24*time.Hour {
		t.Fatalf("purges_at - deleted_at = %s, want 30 days", days)
	}

	restored := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+reply.ID+"/restore", nil, token, nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("send token undelete status=%d body=%s", restored.Code, restored.Body.String())
	}
	if back := listSavedReplies(t, api, admin); len(back) != 1 || back[0].Body != "We refunded you today." {
		t.Fatalf("after undelete list = %+v", back)
	}
	if trash := replyTrash(t, api, admin); len(trash) != 0 {
		t.Fatalf("trash after undelete = %+v, want empty", trash)
	}

	versions := replyVersions(t, api, admin, reply.ID)
	if got, want := versionKinds(versions), []string{"4:undelete", "3:delete", "2:edit", "1:create"}; !equalStrings(got, want) {
		t.Fatalf("versions = %v, want %v (newest first)", got, want)
	}
	for _, version := range versions {
		if version.Token == nil || version.Token.Label != "Reply writer" || version.AgentRequestID != nil {
			t.Errorf("version %d = %+v, want made directly by the Reply writer token", version.Version, version)
		}
	}
	if versions[3].Body != "We refunded you." || versions[2].Body != "We refunded you today." {
		t.Fatalf("version texts = %q, %q", versions[3].Body, versions[2].Body)
	}
}

func TestRestoringAnOldVersionAddsAVersionAndNeverRewritesHistory(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Restore Desk", "admin@restore.example.com")
	id := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Shipping", "body": "Ships in 2 days."})
	patchReply(t, api, admin, id, map[string]any{"body": "Ships in 3 days."})
	patchReply(t, api, admin, id, map[string]any{"title": "Shipping times", "body": "Ships in 5 days."})

	path := "/api/v1/saved-replies/" + id + "/versions/1/restore"
	stale := request(t, api, http.MethodPost, path, map[string]any{"updated_at": "2020-01-01T00:00:00Z"}, admin, nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("restore with a stale updated_at status=%d body=%s, want 409", stale.Code, stale.Body.String())
	}
	body := map[string]any{"updated_at": seenAt(t, api, admin, id)}
	restored := request(t, api, http.MethodPost, path, body, admin, nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore version 1 status=%d body=%s", restored.Code, restored.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, path, body, admin, nil), restored)
	var reply savedReplyView
	decode(t, restored, &reply)
	if reply.Title != "Shipping" || reply.Body != "Ships in 2 days." {
		t.Fatalf("restored reply = %+v, want the text of version 1", reply)
	}

	versions := replyVersions(t, api, admin, id)
	if got, want := versionKinds(versions), []string{"4:restore", "3:edit", "2:edit", "1:create"}; !equalStrings(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	if versions[0].RestoredFromVersion == nil || *versions[0].RestoredFromVersion != 1 || versions[0].Body != "Ships in 2 days." {
		t.Fatalf("restore version = %+v, want a copy of version 1", versions[0])
	}
	if versions[1].Body != "Ships in 5 days." || versions[1].Title != "Shipping times" || versions[2].Body != "Ships in 3 days." {
		t.Fatalf("older versions changed: %+v", versions[1:])
	}
	if missing := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+id+"/versions/9/restore", map[string]any{"updated_at": seenAt(t, api, admin, id)}, admin, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("restore of a version that does not exist status=%d, want 404", missing.Code)
	}
}

func TestTrashKeepsTitlesFreeAndUndeleteRefusesATakenTitle(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Title Desk", "admin@title.example.com")
	first := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Welcome", "body": "Hi"})
	if deleted := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+first, nil, admin, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", deleted.Code)
	}
	if again := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+first, nil, admin, nil); again.Code != http.StatusNotFound {
		t.Fatalf("deleting a reply already in the trash status=%d, want 404", again.Code)
	}
	if edit := request(t, api, http.MethodPatch, "/api/v1/saved-replies/"+first, map[string]any{"body": "x", "updated_at": "2026-01-01T00:00:00Z"}, admin, nil); edit.Code != http.StatusNotFound {
		t.Fatalf("editing a reply in the trash status=%d, want 404", edit.Code)
	}
	create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "welcome", "body": "Hello"})
	undelete := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+first+"/restore", nil, admin, nil)
	if undelete.Code != http.StatusConflict || !strings.Contains(undelete.Body.String(), "duplicate_title") {
		t.Fatalf("undelete over a taken title status=%d body=%s, want 409 duplicate_title", undelete.Code, undelete.Body.String())
	}
	if trash := replyTrash(t, api, admin); len(trash) != 1 {
		t.Fatalf("a refused undelete must leave the reply in the trash: %+v", trash)
	}
}

func TestTrashIsPurgedAfterThirtyDaysAndUndeleteWorksUntilThen(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	admin := createSession(t, api, service, "Purge Desk", "admin@purge.example.com")
	kept := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Kept", "body": "Restored on day 29"})
	gone := create(t, api, admin, "/api/v1/saved-replies", map[string]any{"title": "Gone", "body": "Purged on day 31"})
	for _, id := range []string{kept, gone} {
		if deleted := request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+id, nil, admin, nil); deleted.Code != http.StatusNoContent {
			t.Fatalf("delete status=%d", deleted.Code)
		}
	}
	data := store.New(pool).WithClock(clock.Now)

	clock.set(clock.Now().Add(29 * 24 * time.Hour))
	if purged, err := data.PurgeSavedReplyTrash(context.Background()); err != nil || purged != 0 {
		t.Fatalf("purge on day 29 = %d, %v; want nothing purged", purged, err)
	}
	if restored := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+kept+"/restore", nil, admin, nil); restored.Code != http.StatusOK {
		t.Fatalf("undelete on day 29 status=%d body=%s", restored.Code, restored.Body.String())
	}

	clock.set(clock.Now().Add(2 * 24 * time.Hour))
	if purged, err := data.PurgeSavedReplyTrash(context.Background()); err != nil || purged != 1 {
		t.Fatalf("purge on day 31 = %d, %v; want the one reply left in the trash", purged, err)
	}
	if trash := replyTrash(t, api, admin); len(trash) != 0 {
		t.Fatalf("trash after the purge = %+v", trash)
	}
	if late := request(t, api, http.MethodPost, "/api/v1/saved-replies/"+gone+"/restore", nil, admin, nil); late.Code != http.StatusNotFound {
		t.Fatalf("undelete after the purge status=%d, want 404", late.Code)
	}
	if versions := request(t, api, http.MethodGet, "/api/v1/saved-replies/"+gone+"/versions", nil, admin, nil); versions.Code != http.StatusNotFound {
		t.Fatalf("history of a purged reply status=%d, want 404", versions.Code)
	}
	if listed := listSavedReplies(t, api, admin); len(listed) != 1 || listed[0].ID != kept {
		t.Fatalf("list after the purge = %+v, want only the restored reply", listed)
	}
}

func TestSavedReplyHistoryAndTrashStayInTheirWorkspace(t *testing.T) {
	api, service := testAPI(t)
	owner := createSession(t, api, service, "Owner Desk", "admin@owner-replies.example.com")
	rival := createSession(t, api, service, "Rival Desk", "admin@rival-replies.example.com")
	rivalToken := issueToken(t, api, rival, "Rival writer", "send").Token
	rivalDraft := issueToken(t, api, rival, "Rival drafter", "draft").Token
	live := create(t, api, owner, "/api/v1/saved-replies", map[string]any{"title": "Owner live", "body": "Private"})
	trashed := create(t, api, owner, "/api/v1/saved-replies", map[string]any{"title": "Owner trashed", "body": "Private too"})
	request(t, api, http.MethodDelete, "/api/v1/saved-replies/"+trashed, nil, owner, nil)
	seen := seenAt(t, api, owner, live)

	for _, caller := range []string{rival, rivalToken, rivalDraft} {
		for _, call := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/api/v1/saved-replies/" + live + "/versions", nil},
			{http.MethodGet, "/api/v1/saved-replies/" + trashed + "/versions", nil},
			{http.MethodPost, "/api/v1/saved-replies/" + live + "/versions/1/restore", map[string]any{"updated_at": seen}},
			{http.MethodPost, "/api/v1/saved-replies/" + trashed + "/restore", nil},
			{http.MethodPatch, "/api/v1/saved-replies/" + live, map[string]any{"body": "taken", "updated_at": seen}},
			{http.MethodDelete, "/api/v1/saved-replies/" + live, nil},
		} {
			if response := request(t, api, call.method, call.path, call.body, caller, nil); response.Code != http.StatusNotFound {
				t.Errorf("%s %s from another workspace status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
			}
		}
		if trash := replyTrash(t, api, caller); len(trash) != 0 {
			t.Errorf("another workspace's trash = %+v, want empty", trash)
		}
	}
	if versions := replyVersions(t, api, owner, live); len(versions) != 1 {
		t.Fatalf("owner history changed: %v", versionKinds(versions))
	}
	if trash := replyTrash(t, api, owner); len(trash) != 1 || trash[0].ID != trashed {
		t.Fatalf("owner trash = %+v", trash)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
