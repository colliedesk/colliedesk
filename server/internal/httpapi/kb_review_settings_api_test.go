package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

type kbHelpCenterApproval struct {
	Title           string `json:"title"`
	RequireApproval bool   `json:"require_approval"`
}

// With approval required for the whole help center, an article in a collection that inherits the
// setting is published only once approved. Saving the title alone keeps the setting, and changing
// it needs kb_articles:publish as well as help_center:update.
func TestTheWorkspaceApprovalSettingCoversCollectionsThatInheritIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Approval Setting", "admin@kb-approval-setting.example.com")
	settings := issueCustomToken(t, api, admin, "Settings only", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate).Token
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})

	var center kbHelpCenterApproval
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &center)
	if center.RequireApproval {
		t.Fatalf("a new help center requires approval: %+v", center)
	}
	refused(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Help", "locale": "en", "require_approval": true}, settings, auth.PermKBArticlesPublish)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Help", "locale": "en", "require_approval": true}, admin, http.StatusOK, &center)
	if !center.RequireApproval {
		t.Fatalf("after turning approval on: %+v", center)
	}
	// A save that leaves the setting out keeps it, so a client that only knows the title cannot turn it off.
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Help desk", "locale": "en"}, settings, http.StatusOK, &center)
	if !center.RequireApproval || center.Title != "Help desk" {
		t.Fatalf("after saving the title alone: %+v", center)
	}

	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"},
		admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "approval_required" {
		t.Fatalf("publishing in an inheriting collection: code=%s", code)
	}
	var off kbCollectionView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "require_approval": "off"}, admin, http.StatusOK, &off)
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
}
